import type { EvidenceRecord } from './types';

// Deck proxies only files Marina retains for a run (marina: URIs). Older
// evidence keeps its metadata but has no download.
export function evidenceDownloadURL(taskID: string, evidence: EvidenceRecord): string | undefined {
  if (!evidence.artifact_digest || !evidence.artifact_uri.startsWith('marina:')) return undefined;
  return `/api/v1/operator/tasks/${encodeURIComponent(taskID)}/artifacts/${encodeURIComponent(evidence.artifact_digest)}`;
}
