# Knowledge libraries

Studio users manage private libraries in **Workspace → Knowledge libraries**. Select a library in Chat to search the text it contains. Search uses the application's database and sends matching excerpts to the selected chat model as context. A citation opens the matching text in Studio. Use **Download original** to retrieve the file as uploaded.

| Format | Available in the library |
| --- | --- |
| UTF-8 TXT, Markdown, CSV and TSV | Text search and citations |
| PDF with an embedded text layer | Text search and citations when the server has `pdftotext` |
| DOCX, XLSX and PPTX | Search and citations for document text, sheet cell values, and slide text |
| JPEG, PNG, GIF and WebP | Image preview and original download |
| Scanned PDF | Original download |

Optical character recognition (OCR) is currently unavailable. Office formulas are not recalculated during import. Text indexing covers up to 1 MiB of extracted UTF-8 text and 4,096 search passages per file. The full original is preserved for download and account export. Office imports read at most 16 MiB of relevant expanded XML per file. The Dockerfile installs `pdftotext`. For a standalone binary deployment, put `pdftotext` on the server's `PATH` to enable PDF text extraction. A server without it can still store and serve the PDF original.

## Capacity and storage

An administrator can change both limits in **Admin → Settings → Operation Settings → Knowledge library capacity**. The defaults are 16 MiB per file and 256 MiB per library. The configurable range is 1–64 MiB per file and up to 1,024 MiB per library. The library limit must be at least the file limit. Each library can contain up to 64 files. The limits count original file bytes. Lowering a limit preserves existing files and blocks new uploads that would exceed the new limits.

Originals, extracted text, and search passages are stored in the configured database. Include the database in backups. A standalone or single-node deployment does not require an object store. If large binary uploads dominate database storage or backups, a separate object store can be introduced for originals while keeping ownership, quota accounting and the search index in the database.

Set any reverse proxy request-body limit to at least the configured file limit. When running MySQL, allow sufficient packet size for the largest original and its extracted text in one database write. Each application instance processes one knowledge import at a time to bound peak upload and parsing work.
