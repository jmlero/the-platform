#!/usr/bin/env python3
"""Real-process proof of project drafts, dependency admission and human acceptance."""
import hashlib
import json
import os
import pathlib
import socket
import subprocess
import tempfile
import time
import urllib.error
import urllib.request

with tempfile.TemporaryDirectory(prefix='deck-project-work-') as directory:
    work = pathlib.Path(directory)
    binary = work / 'deck'
    subprocess.run(['go', 'build', '-o', str(binary), './cmd/deck'], cwd=pathlib.Path(__file__).resolve().parents[1], check=True)
    with socket.socket() as sock:
        sock.bind(('127.0.0.1', 0))
        port = sock.getsockname()[1]
    url = f'http://127.0.0.1:{port}'
    process = None
    sequence = 0
    def request(method, path, data=None, expected=200):
        global sequence
        sequence += 1
        headers = {'Content-Type':'application/json', 'Idempotency-Key':f'project-proof-{sequence}'}
        req = urllib.request.Request(url+'/api/v1'+path, method=method, headers=headers, data=None if data is None else json.dumps(data).encode())
        try:
            response = urllib.request.urlopen(req, timeout=10)
        except urllib.error.HTTPError as caught:
            response = caught
        with response:
            body = response.read()
            assert response.status == expected, (method, path, response.status, body.decode())
            return json.loads(body) if body else None
    def start():
        env={key:value for key,value in os.environ.items() if not key.startswith(('DECK_', 'APPDECK_'))}
        env.update(DECK_ADDR=f'127.0.0.1:{port}', DECK_DATA_DIR=str(work/'data'), DECK_DEMO_AGENT='false')
        with (work/'server.log').open('ab') as log:
            proc=subprocess.Popen([str(binary)],env=env,stdout=log,stderr=log)
        for _ in range(100):
            try:
                request('GET','/operator/session')
                return proc
            except (OSError,urllib.error.URLError):
                if proc.poll() is not None: raise AssertionError((work/'server.log').read_text())
                time.sleep(.05)
        proc.terminate();proc.wait(timeout=10)
        raise AssertionError('Deck did not start')
    try:
        process=start()
        project=request('POST','/operator/projects',{'repository':'example/project-work-proof','base_branch':'main'},201)
        assert request('POST','/operator/projects',{'repository':'example/project-work-proof','base_branch':'main'},201)==project
        channel=project['channels'][0]
        message=request('POST',f"/operator/channels/{channel['id']}/messages",{'body':'Capture the result first; refine before authorization.'},201)
        first=request('POST',f"/operator/messages/{message['id']}/promote",{'title':'Verify a bounded result','status':'backlog'},201)
        assert first['project_id']==project['project']['id']
        first=request('PATCH',f"/tasks/{first['id']}",{'version':first['version'],'acceptance_criteria':'Proof fixture validates'})
        second=request('POST','/tasks',{'project_id':first['project_id'],'title':'Use accepted result'},201)
        second=request('PUT',f"/operator/tasks/{second['id']}/dependencies",{'version':second['version'],'depends_on':[first['id']]})
        request('PUT',f"/operator/tasks/{first['id']}/dependencies",{'version':first['version'],'depends_on':[second['id']]},422)
        second=request('POST',f"/tasks/{second['id']}/move",{'status':'ready'})
        assert request('GET','/tasks?status=ready')==[]
        agent=request('POST','/agents',{'id':'proof-agent','installation_id':'proof-installation','name':'Deterministic proof fixture'},201)
        request('POST',f"/tasks/{second['id']}/claim",{'agent_id':agent['id'],'expected_task_version':second['version'],'lease_seconds':300},409)
        first=request('POST',f"/tasks/{first['id']}/move",{'status':'ready'})
        run=request('POST',f"/tasks/{first['id']}/claim",{'agent_id':agent['id'],'expected_task_version':first['version'],'lease_seconds':300},201)
        base=f"/runs/{run['id']}"
        def guard():
            current=request('GET',f"/operator/tasks/{first['id']}")['runs'][0]
            return {'agent_id':agent['id'],'expected_version':current['version'],'fence_token':current['fence_token']}
        plan=request('POST',base+'/plan',{**guard(),'plan':'Run deterministic fixture validation','require_approval':False})['plan_revision']
        request('POST',base+'/verify',guard(),204)
        digest=hashlib.sha256(b'Proof fixture validates').hexdigest()
        evidence=request('POST',base+'/evidence',{**guard(),'id':'evidence-project-proof','plan_revision_id':plan['id'],'type':'acceptance','producer':'proof-worker','verifier':'proof-verifier','subject_digest':digest,'source_revision':'fixture-v1','command':'synthetic fixture proof','outcome':'passed','provenance_uri':'test://project-work/provenance','provenance_digest':'sha256:fixture-provenance','observed_at':int(time.time()*1000),'artifact_uri':'test://project-work/result','artifact_digest':'sha256:fixture-result'},201)
        request('POST',base+'/complete',{**guard(),'summary':'Deterministic proof result; no model or repository mutation','evidence_ids':[evidence['id']]},204)
        first=request('GET',f"/operator/tasks/{first['id']}")['task']
        assert first['status']=='done' and not first.get('review')
        assert request('GET','/tasks?status=ready')==[]
        acceptance={'version':first['version'],'source_available':False}
        accepted=request('POST',f"/operator/tasks/{first['id']}/accept",acceptance)
        assert accepted['review']['run_id']==run['id'] and accepted['review']['accepted_by']=='local'
        assert request('POST',f"/operator/tasks/{first['id']}/accept",acceptance)==accepted
        assert [t['id'] for t in request('GET','/tasks?status=ready')]==[second['id']]
        process.terminate();process.wait(timeout=10);process=start()
        restored=request('GET',f"/operator/tasks/{first['id']}")['task']
        assert restored['review']==accepted['review']
        assert request('GET',f"/operator/tasks/{second['id']}")['task']['depends_on']==[first['id']]
        assert [t['id'] for t in request('GET','/tasks?status=ready')]==[second['id']]
        print('PASS: real Deck project/channel replay, draft promotion, cycle rejection, discovery/direct-claim gates, verified-result versus human acceptance, acceptance replay, dependency release and process restart.')
    finally:
        if process is not None and process.poll() is None:
            process.terminate();process.wait(timeout=10)
