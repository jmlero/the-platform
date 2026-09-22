"""Exercise the installed LangChain/LangGraph harness through Responses HTTP."""
import json
import threading
import unittest
from http.server import BaseHTTPRequestHandler, HTTPServer

from run import diagnose


class HarnessTest(unittest.TestCase):
    def test_uses_gateway_responses_with_no_external_tools_or_state(self):
        requests = []
        report = {"summary": "One provider outage", "findings": [{"code": "provider_unavailable", "source_id": "outage", "quote": "HTTP 503", "explanation": "Provider recovery required."}]}

        class Gateway(BaseHTTPRequestHandler):
            def log_message(self, *_):
                pass

            def do_POST(self):
                body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
                requests.append((self.path, self.headers.get("Authorization"), body))
                response = {"id": "resp_fixture", "object": "response", "created_at": 1, "status": "completed", "model": "fixture", "output": [{"type": "function_call", "id": "fc_fixture", "call_id": "call_fixture", "name": "DiagnosticReport", "arguments": json.dumps(report), "status": "completed"}], "usage": {"input_tokens": 20, "output_tokens": 20, "total_tokens": 40}}
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.end_headers()
                self.wfile.write(json.dumps(response).encode())

        server = HTTPServer(("127.0.0.1", 0), Gateway)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            result = diagnose({"version": 1, "profile_digest": "a" * 64, "input_digest": "b" * 64, "sources": [{"id": "outage", "content": "upstream HTTP 503"}], "model": "fixture", "model_url": f"http://127.0.0.1:{server.server_port}/model/v1"}, "attempt-only-token")
        finally:
            server.shutdown()
            server.server_close()
            thread.join()
        self.assertEqual(result, report)
        self.assertEqual(len(requests), 1)
        path, auth, body = requests[0]
        self.assertEqual(path, "/model/v1/responses")
        self.assertEqual(auth, "Bearer attempt-only-token")
        self.assertFalse(body.get("store", False))
        self.assertNotIn("previous_response_id", body)
        self.assertEqual([t["name"] for t in body["tools"]], ["DiagnosticReport"])
        self.assertNotIn("expected", json.dumps(body))



class ContextToolHarnessTest(unittest.TestCase):
    def test_calls_only_the_attempt_context_tool_and_preserves_provenance(self):
        requests = []
        report = {"summary": "One outage", "findings": [{"code": "provider_unavailable", "source_id": "outage", "quote": "HTTP 503", "explanation": "Provider recovery required"}]}
        class Gateway(BaseHTTPRequestHandler):
            def log_message(self, *_):
                pass
            def do_POST(self):
                body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
                requests.append((self.path, self.headers.get("Authorization"), body))
                if self.path.endswith("/tools/read-context-source"):
                    response = {"uri": body["uri"], "content": "Preserve source quotes", "trust": "trusted", "revision": "1", "digest": "sha256:" + "c" * 64}
                else:
                    number = sum(1 for path, _, _ in requests if path.endswith("/responses"))
                    name, arguments = ("read_context_source", {"uri": "context://project/rules/1"}) if number == 1 else ("DiagnosticReport", report)
                    response = {"id": f"resp_{number}", "object": "response", "created_at": 1, "status": "completed", "model": "fixture", "output": [{"type": "function_call", "id": f"fc_{number}", "call_id": f"call_{number}", "name": name, "arguments": json.dumps(arguments), "status": "completed"}], "usage": {"input_tokens": 20, "output_tokens": 20, "total_tokens": 40}}
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.end_headers()
                self.wfile.write(json.dumps(response).encode())
        server = HTTPServer(("127.0.0.1", 0), Gateway)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            base = f"http://127.0.0.1:{server.server_port}/workers/attempt"
            result = diagnose({"version": 1, "sources": [{"id": "outage", "content": "HTTP 503"}], "model": "fixture", "model_url": base + "/model/v1", "tools": ["read_context_source"], "tool_url": base + "/tools/read-context-source", "context": {"digest": "sha256:" + "b" * 64, "inputs": [{"uri": "context://project/rules/1", "trust": "trusted"}]}}, "attempt-token")
        finally:
            server.shutdown()
            server.server_close()
            thread.join()
        self.assertEqual(result, report)
        self.assertEqual(len(requests), 3)
        self.assertTrue(requests[1][0].endswith("/tools/read-context-source"))
        self.assertEqual(requests[1][1], "Bearer attempt-token")
        self.assertEqual(requests[1][2], {"uri": "context://project/rules/1"})
        self.assertIn("Preserve source quotes", json.dumps(requests[2][2]))
        self.assertIn("trusted", json.dumps(requests[2][2]))

if __name__ == "__main__":
    unittest.main()
