# Account privacy, memory, and data deletion

This guide describes the A1O Platform features for users and site operators. It covers the primary database and the application's files. Model providers and separately configured tools may keep their own copies. Deployment backups and infrastructure logs have separate retention.

## Users

- In **Account → Privacy**, download a ZIP of your own profile, active chats and their messages, attachments, knowledge documents, Work task metadata and transcripts, agents, skills, media, memory, usage and payment orders. The archive omits deleted chats, stored API and access keys, password hashes, and saved MCP authentication settings. Text you typed into a conversation is included verbatim. Metadata uses JSON or JSON Lines. Folders contain original file bytes named by their record IDs. `manifest.json` lists missing file payloads, if any.
- Long-term memory starts off for each account, including existing accounts. Enable it in **Account → Privacy** to let Chat and Work recall and record notes when the operator also enables memory for the site. Turning it off blocks recall and recording on subsequent tool calls. Existing notes remain available in the privacy page, where you can remove one or clear the store. Clearing memory does not rewrite previous chats that already contain a recalled fact.
- Deleting a chat immediately hides it from Chat and excludes it from subsequent account exports. The application schedules its messages and associated runs for permanent deletion after 15 days. Standalone attachment files are managed independently in Workspace and may be reused in other chats.
- Download your data before deleting your account. Account deletion revokes access, removes profile identifiers, and clears long-term memory. If removing memory files fails, automated cleanup retries. Existing chats are scheduled for permanent deletion after 15 days, with any earlier deletion deadlines kept. After the retention period, automatic cleanup removes account-related messages, Work records, uploaded files, knowledge libraries, saved memory, and generated media. Financial records may remain associated with a pseudonymous account ID. The root operator account cannot be deleted from the app.

## Site operators

The former chat review page and API have been removed. The database migration drops the former review-access log table.

1. The primary application process (a node without `NODE_TYPE=slave`) starts cleanup at launch and schedules a pass every minute. Once the 15-day retention period has elapsed, the next successful cleanup of each record permanently deletes it. Chats and accounts advance through separate batches. When one record fails or a batch times out, later passes continue through the batches and retry failed items. Check the server log for cleanup errors. In a multi-node deployment, the cleanup process needs access to the application's shared attachment, media, and memory directories.
2. Apply an independent retention policy to backups and infrastructure logs. Before bringing a restored deployment back online, reconcile deletion records created after the backup to keep deleted chats out of the restored interface and exports.

Automatic cleanup requires a recorded purge deadline. The application records that deadline when a chat or account is deleted while the retention feature is installed.
