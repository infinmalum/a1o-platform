package model

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"gorm.io/gorm"
)

// WritePrivacyExport writes portable JSON Lines, binary files, and a manifest.
func WritePrivacyExport(ctx context.Context, dst io.Writer, userID int) error {
	db := DB.WithContext(ctx)
	var profile User
	if err := db.Select("id", "username", "display_name", "email", "group", "role", "quota", "used_quota", "request_count", "memory_enabled").
		Where("status = ?", UserStatusEnabled).Take(&profile, userID).Error; err != nil {
		return err
	}
	archive := zip.NewWriter(dst)
	if err := archiveJSON(archive, "profile.json", map[string]any{
		"id": profile.Id, "username": profile.Username, "display_name": profile.DisplayName,
		"email": profile.Email, "group": profile.Group, "role": profile.Role, "quota": profile.Quota,
		"used_quota": profile.UsedQuota, "request_count": profile.RequestCount, "memory_enabled": profile.MemoryEnabled,
	}); err != nil {
		return err
	}

	owned := func() *gorm.DB { return db.Where("user_id = ?", userID) }
	sessions := db.Model(&ChatSession{}).Select("id").Where("user_id = ? AND privacy_purge_at = 0", userID)
	libraries := db.Model(&KnowledgeLibrary{}).Select("id").Where("user_id = ?", userID)
	agents := db.Model(&AgentDefinition{}).Select("id").Where("owner_id = ?", userID)

	for _, write := range []func() error{
		func() error {
			return exportRows[ChatSession](archive, "chats", owned().Where("privacy_purge_at = 0"), nil)
		},
		func() error {
			return exportRows[ChatMessage](archive, "messages", db.Where("session_id IN (?) AND role IN ?", sessions, []string{ChatRoleUser, ChatRoleAssistant}).
				Select("id", "session_id", "role", "content", "parts", "sources", "created_time"), nil)
		},
		func() error { return exportRows[KnowledgeLibrary](archive, "knowledge-libraries", owned(), nil) },
		func() error {
			return exportRows[KnowledgeDocument](archive, "knowledge-documents", db.Where("library_id IN (?)", libraries),
				func(doc *KnowledgeDocument) error {
					dst, err := archive.Create(fmt.Sprintf("knowledge/%d", doc.Id))
					if err != nil {
						return err
					}
					_, err = dst.Write(doc.Body)
					return err
				})
		},
		func() error { return exportRows[WorkTask](archive, "work-tasks", owned(), nil) },
		func() error {
			return exportRows[AgentRun](archive, "work-runs", owned().Where("kind = ?", "work").
				Select("id", "user_id", "task_id", "model", "kind", "status", "instructions", "created_time", "updated_time"),
				func(run *AgentRun) error { return exportWorkTranscript(db, archive, userID, run.Id) })
		},
		func() error {
			return exportRows[RunInput](archive, "work-inputs", owned().Omit("idempotency_key"), nil)
		},

		func() error { return exportRows[ChatSkill](archive, "skills", db.Where("owner_id = ?", userID), nil) },
		func() error {
			return exportRows[ChatMCPServer](archive, "mcp-servers", db.Where("owner_id = ?", userID).
				Select("id", "owner_id", "name", "transport", "enabled", "created_time", "updated_time"), nil)
		},
		func() error {
			return exportRows[AgentDefinition](archive, "agents", db.Where("owner_id = ?", userID), nil)
		},
		func() error {
			return exportRows[AgentVersion](archive, "agent-versions", db.Where("definition_id IN (?)", agents), nil)
		},
		func() error {
			return exportRows[ImageGeneration](archive, "media-jobs", owned().Select("id", "user_id", "model", "prompt",
				"video_duration", "size", "quality", "n", "status", "created_time", "updated_time"), nil)
		},
		func() error { return exportRows[PaymentOrder](archive, "orders", owned().Omit("checkout_url"), nil) },
		func() error {
			return exportRows[Log](archive, "usage", LOG_DB.WithContext(ctx).Where("user_id = ?", userID).
				Select("id", "user_id", "type", "quota", "model_name", "prompt_tokens", "completion_tokens", "created_at"), nil)
		},
	} {
		if err := write(); err != nil {
			return err
		}
	}

	var unavailable []string
	addFile := func(category, root string, id int, path string) error {
		name := fmt.Sprintf("%s/%d", category, id)
		if err := archiveFile(archive, root, path, name); err != nil {
			if !os.IsNotExist(err) {
				return err
			}
			unavailable = append(unavailable, name)
		}
		return nil
	}
	if err := exportRows[ChatFile](archive, "attachments", owned(), func(file *ChatFile) error {
		return addFile("attachments", chatFileRoot(), file.Id, file.Path)
	}); err != nil {
		return err
	}
	if err := exportRows[ImageAsset](archive, "media", owned(), func(asset *ImageAsset) error {
		return addFile("media", imageAssetRoot(), asset.Id, asset.Path)
	}); err != nil {
		return err
	}
	_, memories, err := ListUserMemory(ctx, userID)
	if err != nil {
		return err
	}
	if err := archiveJSON(archive, "memory.json", memories); err != nil {
		return err
	}
	if err := archiveJSON(archive, "manifest.json", map[string]any{"version": 1, "unavailable_files": unavailable}); err != nil {
		return err
	}
	return archive.Close()
}

// Export the same user/assistant text shown in Work, omitting provider
// state, system messages, tool calls and tool output from stored runs.
func exportWorkTranscript(db *gorm.DB, archive *zip.Writer, userID, runID int) error {
	var run AgentRun
	if err := db.Select("context_items", "input_items").Where("user_id = ? AND kind = ?", userID, "work").
		Take(&run, runID).Error; err != nil {
		return err
	}
	raw := run.ContextItems
	if raw == nil || *raw == "" || *raw == "[]" {
		raw = run.InputItems
	}
	var items []struct {
		Type    string `json:"type"`
		Role    string `json:"role"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if raw != nil && *raw != "" {
		if err := json.Unmarshal([]byte(*raw), &items); err != nil {
			return err
		}
	}
	messages := []ReviewMessage{}
	for _, item := range items {
		if item.Type != "message" || (item.Role != ChatRoleUser && item.Role != ChatRoleAssistant) {
			continue
		}
		var text strings.Builder
		for _, part := range item.Content {
			if part.Type == "input_text" || part.Type == "output_text" {
				text.WriteString(part.Text)
			}
		}
		messages = append(messages, ReviewMessage{Role: item.Role, Content: text.String()})
	}
	return archiveJSON(archive, fmt.Sprintf("work-transcripts/%d.json", runID), messages)
}

func archiveJSON(archive *zip.Writer, name string, value any) error {
	entry, err := archive.Create(name)
	if err != nil {
		return err
	}
	return json.NewEncoder(entry).Encode(value)
}

func exportRows[T any](archive *zip.Writer, name string, query *gorm.DB, assets func(*T) error) error {
	// Keep memory bounded and write the manifest as a single JSON Lines entry.
	// ZIP entries cannot be interleaved, so file bytes use a second pass.
	entry, err := archive.Create(name + ".jsonl")
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(entry)
	var rows []T
	if err := query.Session(&gorm.Session{}).FindInBatches(&rows, 100, func(_ *gorm.DB, _ int) error {
		for i := range rows {
			if file, ok := any(&rows[i]).(*ChatFile); ok {
				file.Path = ""
			}
			if err := encoder.Encode(rows[i]); err != nil {
				return err
			}
		}
		return nil
	}).Error; err != nil || assets == nil {
		return err
	}
	return query.Session(&gorm.Session{}).FindInBatches(&rows, 100, func(_ *gorm.DB, _ int) error {
		for i := range rows {
			if err := assets(&rows[i]); err != nil {
				return err
			}
		}
		return nil
	}).Error
}

func archiveFile(archive *zip.Writer, rootPath, path, entry string) error {
	if path == "" {
		return os.ErrNotExist
	}
	filename, err := storedFilename(rootPath, path)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return err
	}
	defer root.Close()
	file, err := root.Open(filename)
	if err != nil {
		return err
	}
	defer file.Close()
	dst, err := archive.CreateHeader(&zip.FileHeader{Name: entry, Method: zip.Store})
	if err != nil {
		return err
	}
	_, err = io.Copy(dst, file)
	return err
}
