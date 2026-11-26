package controller

import (
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"gopkg.in/yaml.v3"
)

const maxSkillMarkdownBytes = 64 << 10

var skillMarkdownName = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

type skillMarkdownDraft struct {
	Name         string `json:"name" yaml:"name"`
	Description  string `json:"description" yaml:"description"`
	Instructions string `json:"instructions" yaml:"-"`
}

func parseSkillMarkdown(markdown string) (skillMarkdownDraft, bool) {
	var draft skillMarkdownDraft
	if !utf8.ValidString(markdown) {
		return draft, false
	}
	markdown = strings.ReplaceAll(strings.TrimPrefix(markdown, "\uFEFF"), "\r\n", "\n")
	header, ok := strings.CutPrefix(markdown, "---\n")
	if !ok {
		return draft, false
	}
	header, body, ok := strings.Cut(header, "\n---\n")
	if !ok || yaml.Unmarshal([]byte(header), &draft) != nil {
		return draft, false
	}
	draft.Description = strings.TrimSpace(draft.Description)
	draft.Instructions = strings.TrimSpace(body)
	return draft, len(draft.Name) <= 64 && skillMarkdownName.MatchString(draft.Name) &&
		draft.Description != "" && utf8.RuneCountInString(draft.Description) <= 1024 && draft.Instructions != ""
}

// PreviewSkillMarkdown parses a single SKILL.md into an editable draft.
func PreviewSkillMarkdown(c *gin.Context) {
	raw, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, maxSkillMarkdownBytes))
	var oversized *http.MaxBytesError
	if errors.As(err, &oversized) {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"success": false, "code": "skill_markdown_too_large", "message": "SKILL.md exceeds 64 KiB"})
		return
	}
	draft, valid := parseSkillMarkdown(string(raw))
	if err != nil || !valid {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "code": "skill_markdown_invalid", "message": "SKILL.md requires valid YAML frontmatter with name and description, followed by instructions"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": draft})
}
