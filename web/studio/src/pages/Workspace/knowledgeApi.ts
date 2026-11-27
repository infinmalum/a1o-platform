import { pageQuery } from '@/components/StudioList';
import { studioRequest } from '@/helpers/studioRequest';

export interface KnowledgeLibrary {
  id: number;
  name: string;
  size: number;
  document_count: number;
}

export interface KnowledgeDocument {
  id: number;
  name: string;
  size: number;
  content_type?: string;
}

export interface KnowledgeUploadRules {
  file_bytes: number;
  library_bytes: number;
  max_documents: number;
  extensions: string[];
}

export interface KnowledgeSource {
  label: string;
  document_id: number;
  name: string;
  heading?: string;
  start_line: number;
  end_line: number;
}

const request = <T>(path: string, init?: RequestInit) => studioRequest<T>(path, init, false);


const base = '/api/knowledge';

export function knowledgeFileUrl(id: number, preview = false) {
  return `${base}/documents/${id}/file${preview ? '?preview=1' : ''}`;
}

export const knowledgeApi = {
  uploadRules: () => request<KnowledgeUploadRules>(base + '/upload-rules'),
  list: () => request<KnowledgeLibrary[]>(base + '/libraries'),
  listPage: (page: number) => request<KnowledgeLibrary[]>(base + '/libraries?' + pageQuery(page)),
  create: (name: string) => request<KnowledgeLibrary>(base + '/libraries', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name }),
  }),
  documents: (id: number) => request<KnowledgeDocument[]>(`${base}/libraries/${id}/documents`),
  upload: (id: number, file: File) => request<KnowledgeDocument>(
    `${base}/libraries/${id}/documents?${new URLSearchParams({ name: file.name })}`, {
      method: 'POST', headers: { 'Content-Type': 'application/octet-stream' }, body: file,
    },
  ),
  document: (id: number) => request<{ document: KnowledgeDocument; text: string }>(`${base}/documents/${id}`),
  deleteDocument: (libraryId: number, documentId: number) =>
    request<void>(`${base}/libraries/${libraryId}/documents/${documentId}`, { method: 'DELETE' }),
  deleteLibrary: (id: number) => request<void>(`${base}/libraries/${id}`, { method: 'DELETE' }),
  selectForChat: (sessionId: number, libraryId: number) => request<void>(`/api/chat/sessions/${sessionId}/knowledge`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ library_id: libraryId }),
  }),
};

export function knowledgeSourceUrl(documentId: number, startLine?: number, endLine?: number) {
  const params = new URLSearchParams({ document: String(documentId) });
  if (startLine && endLine) {
    params.set('start', String(startLine));
    params.set('end', String(endLine));
  }
  return '/workspace?' + params;
}
