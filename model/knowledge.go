package model

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/infinmalum/a1o-platform/common/helper"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	KnowledgeMaxDocuments = 64
	knowledgeMaxChunks    = 4096
	knowledgeChunkRunes   = 400
	knowledgeMaxResults   = 5
)

var (
	ErrKnowledgeNotFound        = errors.New("资料库或来源不存在")
	ErrKnowledgeInvalidDocument = errors.New("无效的资料文件")
	ErrKnowledgeLimit           = errors.New("资料库容量不足")
	ErrKnowledgeFileTooLarge    = errors.New("资料文件超过大小上限")
)

type KnowledgeLibrary struct {
	Id            int    `json:"id"`
	UserId        int    `json:"-" gorm:"index"`
	Name          string `json:"name" gorm:"size:120"`
	Size          int    `json:"size"`
	DocumentCount int    `json:"document_count"`
	CreatedTime   int64  `json:"created_time"`
}

type KnowledgeDocument struct {
	Id            int    `json:"id"`
	LibraryId     int    `json:"library_id" gorm:"index"`
	Name          string `json:"name" gorm:"size:255"`
	Size          int    `json:"size"`
	CreatedTime   int64  `json:"created_time"`
	Body          []byte `json:"-"`
	ContentType   string `json:"content_type,omitempty" gorm:"size:120"`
	ExtractedText string `json:"-"`
}

type KnowledgeChunk struct {
	Id         int
	DocumentId int `gorm:"index"`
	LibraryId  int `gorm:"index"`
	Heading    string
	StartLine  int
	EndLine    int
	Text       string `gorm:"type:text"`
}

type KnowledgeCitation struct {
	Label      string `json:"label"`
	DocumentId int    `json:"document_id"`
	Name       string `json:"name"`
	Heading    string `json:"heading,omitempty"`
	StartLine  int    `json:"start_line"`
	EndLine    int    `json:"end_line"`
}

type KnowledgeMatch struct {
	KnowledgeCitation
	Text string
}

func ListKnowledgeLibraries(ctx context.Context, userId int) ([]KnowledgeLibrary, error) {
	return ListKnowledgeLibrariesPage(ctx, userId, ListPage{})
}

func ListKnowledgeLibrariesPage(ctx context.Context, userId int, page ListPage) ([]KnowledgeLibrary, error) {
	libraries := []KnowledgeLibrary{}
	err := page.Apply(DB.WithContext(ctx).Where("user_id = ?", userId).Order("id DESC")).Find(&libraries).Error
	return libraries, err
}

func CreateKnowledgeLibrary(ctx context.Context, userId int, name string) (*KnowledgeLibrary, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 120 {
		return nil, errors.New("资料库名称须为 1–120 个字符")
	}
	library := &KnowledgeLibrary{UserId: userId, Name: name, CreatedTime: helper.GetTimestamp()}
	return library, DB.WithContext(ctx).Create(library).Error
}

func ListKnowledgeDocuments(ctx context.Context, userId, libraryId int) ([]KnowledgeDocument, error) {
	db := DB.WithContext(ctx)
	if err := getKnowledgeLibrary(db, userId, libraryId); err != nil {
		return nil, err
	}
	documents := []KnowledgeDocument{}
	err := db.Omit("body", "extracted_text").Where("library_id = ?", libraryId).Order("id DESC").Find(&documents).Error
	return documents, err
}

func GetKnowledgeDocument(ctx context.Context, userId, documentId int) (*KnowledgeDocument, error) {
	return getKnowledgeDocument(ctx, userId, documentId, "extracted_text")
}

func GetKnowledgeDocumentPreview(ctx context.Context, userId, documentId int) (*KnowledgeDocument, string, error) {
	document, err := getKnowledgeDocument(ctx, userId, documentId, "body")
	if err != nil {
		return nil, "", err
	}
	if document.ContentType != "" {
		return document, document.ExtractedText, nil
	}
	// Records created before binary imports contain the UTF-8 text in Body.
	legacy, err := GetKnowledgeDocument(ctx, userId, documentId)
	if err != nil {
		return nil, "", err
	}
	return legacy, string(legacy.Body), nil
}

func getKnowledgeDocument(ctx context.Context, userId, documentId int, omit string) (*KnowledgeDocument, error) {
	var document KnowledgeDocument
	err := DB.WithContext(ctx).
		Joins("JOIN knowledge_libraries ON knowledge_libraries.id = knowledge_documents.library_id").
		Where("knowledge_documents.id = ? AND knowledge_libraries.user_id = ?", documentId, userId).
		Omit(omit).Take(&document).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrKnowledgeNotFound
	}
	return &document, err
}

func ImportKnowledgeDocument(ctx context.Context, userId, libraryId int, name string, body []byte) (*KnowledgeDocument, error) {
	name = filepath.Base(strings.ReplaceAll(strings.TrimSpace(name), "\\", "/"))
	ext := strings.ToLower(filepath.Ext(name))
	contentType, supported := knowledgeFormats[ext]
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) > 255 || !supported {
		return nil, ErrKnowledgeInvalidDocument
	}
	limits := GetKnowledgeLimits()
	if len(body) > limits.FileBytes() {
		return nil, ErrKnowledgeFileTooLarge
	}
	if err := getKnowledgeLibrary(DB.WithContext(ctx), userId, libraryId); err != nil {
		return nil, err
	}
	text, err := knowledgeText(ctx, ext, body)
	if err != nil {
		return nil, err
	}
	chunks := splitKnowledgeText(text, ext == ".md")
	if len(chunks) == 0 && strings.HasPrefix(contentType, "text/") {
		return nil, ErrKnowledgeInvalidDocument
	}
	chunks = chunks[:min(len(chunks), knowledgeMaxChunks)]
	document := &KnowledgeDocument{
		LibraryId: libraryId, Name: name, Size: len(body), Body: body, CreatedTime: helper.GetTimestamp(),
		ContentType: contentType, ExtractedText: text,
	}
	err = DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&KnowledgeLibrary{}).
			Where("id = ? AND user_id = ? AND size <= ? AND document_count < ?",
				libraryId, userId, limits.LibraryBytes()-document.Size, KnowledgeMaxDocuments).
			Updates(map[string]any{
				"size": gorm.Expr("size + ?", document.Size), "document_count": gorm.Expr("document_count + 1"),
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			if err := getKnowledgeLibrary(tx, userId, libraryId); err != nil {
				return err
			}
			return ErrKnowledgeLimit
		}
		if err := tx.Create(document).Error; err != nil {
			return err
		}
		for i := range chunks {
			chunks[i].LibraryId, chunks[i].DocumentId = libraryId, document.Id
		}
		if len(chunks) > 0 {
			return tx.CreateInBatches(&chunks, 100).Error
		}
		return nil
	})
	return document, err
}

func DeleteKnowledgeDocument(ctx context.Context, userId, libraryId, documentId int) error {
	return DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := getKnowledgeLibrary(tx.Clauses(clause.Locking{Strength: "UPDATE"}), userId, libraryId); err != nil {
			return err
		}
		var document KnowledgeDocument
		err := tx.Select("id", "size").Where("id = ? AND library_id = ?", documentId, libraryId).Take(&document).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrKnowledgeNotFound
		}
		if err != nil {
			return err
		}
		if err := tx.Where("document_id = ? AND library_id = ?", documentId, libraryId).Delete(&KnowledgeChunk{}).Error; err != nil {
			return err
		}
		if err := tx.Delete(&document).Error; err != nil {
			return err
		}
		return tx.Model(&KnowledgeLibrary{}).Where("id = ?", libraryId).Updates(map[string]any{
			"size": gorm.Expr("size - ?", document.Size), "document_count": gorm.Expr("document_count - 1"),
		}).Error
	})
}

func DeleteKnowledgeLibrary(ctx context.Context, userId, libraryId int) error {
	return DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Where("id = ? AND user_id = ?", libraryId, userId).Delete(&KnowledgeLibrary{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrKnowledgeNotFound
		}
		if err := tx.Model(&ChatSession{}).Where("user_id = ? AND knowledge_library_id = ?", userId, libraryId).
			Update("knowledge_library_id", 0).Error; err != nil {
			return err
		}
		if err := tx.Where("library_id = ?", libraryId).Delete(&KnowledgeChunk{}).Error; err != nil {
			return err
		}
		return tx.Where("library_id = ?", libraryId).Delete(&KnowledgeDocument{}).Error
	})
}

func SetChatKnowledgeLibrary(ctx context.Context, userId, sessionId, libraryId int) error {
	return DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if libraryId != 0 {
			if err := getKnowledgeLibrary(tx.Clauses(clause.Locking{Strength: "UPDATE"}), userId, libraryId); err != nil {
				return err
			}
		}
		var session ChatSession
		err := tx.Select("id").Where("id = ? AND user_id = ?", sessionId, userId).Take(&session).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrKnowledgeNotFound
		}
		if err != nil {
			return err
		}
		return tx.Model(&session).Where("user_id = ?", userId).Update("knowledge_library_id", libraryId).Error
	})
}

func getKnowledgeLibrary(db *gorm.DB, userId, libraryId int) error {
	var library KnowledgeLibrary
	err := db.Select("id").Where("id = ? AND user_id = ?", libraryId, userId).Take(&library).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrKnowledgeNotFound
	}
	return err
}

func splitKnowledgeText(body string, markdown bool) []KnowledgeChunk {
	var chunks []KnowledgeChunk
	var buffer strings.Builder
	count, first, last := 0, 0, 0
	heading := ""
	flush := func() {
		if text := strings.TrimSpace(buffer.String()); text != "" {
			chunks = append(chunks, KnowledgeChunk{Heading: heading, Text: text, StartLine: first, EndLine: last})
		}
		buffer.Reset()
		count = 0
	}
	for index, raw := range strings.Split(body, "\n") {
		if len(chunks) >= knowledgeMaxChunks {
			return chunks
		}
		line := strings.TrimSpace(strings.TrimPrefix(raw, "\ufeff"))
		if line == "" {
			if count >= knowledgeChunkRunes/2 {
				flush()
			}
			continue
		}
		if markdown && strings.HasPrefix(line, "#") {
			marker := len(line) - len(strings.TrimLeft(line, "#"))
			if marker <= 6 && strings.HasPrefix(line[marker:], " ") {
				flush()
				name := []rune(strings.TrimSpace(line[marker:]))
				heading = string(name[:min(len(name), 120)])
			}
		}
		for _, char := range line {
			if count >= knowledgeChunkRunes {
				flush()
			}
			if count == 0 {
				first = index + 1
			}
			last = index + 1
			buffer.WriteRune(char)
			count++
		}
		if count > 0 {
			buffer.WriteByte('\n')
			count++
		}
	}
	flush()
	return chunks
}

func SearchKnowledge(ctx context.Context, userId, libraryId int, query string) ([]KnowledgeMatch, error) {
	terms := knowledgeTerms(query)
	if len(terms) == 0 {
		return nil, nil
	}
	rows, err := DB.WithContext(ctx).Model(&KnowledgeChunk{}).
		Select("knowledge_chunks.document_id, knowledge_documents.name, knowledge_chunks.heading, knowledge_chunks.start_line, knowledge_chunks.end_line, knowledge_chunks.text").
		Joins("JOIN knowledge_libraries ON knowledge_libraries.id = knowledge_chunks.library_id").
		Joins("JOIN knowledge_documents ON knowledge_documents.id = knowledge_chunks.document_id").
		Where("knowledge_libraries.user_id = ? AND knowledge_chunks.library_id = ?", userId, libraryId).
		Order("knowledge_chunks.id").Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type scoredMatch struct {
		KnowledgeMatch
		score int
	}
	ranked := make([]scoredMatch, 0, knowledgeMaxResults+1)
	for rows.Next() {
		var hit KnowledgeMatch
		if err := rows.Scan(&hit.DocumentId, &hit.Name, &hit.Heading, &hit.StartLine, &hit.EndLine, &hit.Text); err != nil {
			return nil, err
		}
		text := strings.ToLower(hit.Text)
		title := strings.ToLower(hit.Heading + " " + hit.Name)
		score := 0
		for _, term := range terms {
			if strings.Contains(text, term) {
				score++
			}
			if strings.Contains(title, term) {
				score += 2
			}
		}
		if score == 0 || len(ranked) == knowledgeMaxResults && score <= ranked[len(ranked)-1].score {
			continue
		}
		ranked = append(ranked, scoredMatch{hit, score})
		slices.SortStableFunc(ranked, func(a, b scoredMatch) int { return b.score - a.score })
		ranked = ranked[:min(len(ranked), knowledgeMaxResults)]
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	matches := make([]KnowledgeMatch, 0, len(ranked))
	for _, hit := range ranked {
		matches = append(matches, hit.KnowledgeMatch)
	}
	return matches, nil
}

func knowledgeTerms(query string) []string {
	query = strings.TrimSpace(query)
	count := 0
	for index := range query {
		if count == 256 {
			query = query[:index]
			break
		}
		count++
	}
	query = strings.ToLower(query)
	var terms []string
	seen := make(map[string]bool)
	add := func(term string) {
		if utf8.RuneCountInString(term) >= 2 && !seen[term] && len(terms) < 64 {
			seen[term] = true
			terms = append(terms, term)
		}
	}
	add(query)
	var word strings.Builder
	var previous rune
	for _, char := range query {
		if unicode.In(char, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul) {
			add(word.String())
			word.Reset()
			if previous != 0 {
				add(string([]rune{previous, char}))
			}
			previous = char
			continue
		}
		previous = 0
		if unicode.IsLetter(char) || unicode.IsNumber(char) || strings.ContainsRune("-_", char) {
			word.WriteRune(char)
		} else {
			add(strings.Trim(word.String(), "-_"))
			word.Reset()
		}
	}
	add(strings.Trim(word.String(), "-_"))
	return terms
}
