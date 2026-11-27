package model

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"os/exec"
	"strings"
	"testing"

	"github.com/infinmalum/a1o-platform/common/config"
	"github.com/stretchr/testify/require"
)

func TestKnowledgeLibraryOwnershipRetrievalAndDeletion(t *testing.T) {
	previous := DB
	DB = openAgentRunTestDB(t)
	t.Cleanup(func() { DB = previous })
	require.NoError(t, DB.AutoMigrate(&ChatSession{}, &KnowledgeLibrary{}, &KnowledgeDocument{}, &KnowledgeChunk{}))

	ctx := context.Background()
	library, err := CreateKnowledgeLibrary(ctx, 1, "手册")
	require.NoError(t, err)
	document, err := ImportKnowledgeDocument(ctx, 1, library.Id, "手册.md", []byte("# 退款流程\n退款须在七天内申请，出错时记录 ERR-042。\n# 登录\n使用账号登录。"))
	require.NoError(t, err)
	_, err = ImportKnowledgeDocument(ctx, 2, library.Id, "private.txt", []byte("不可写入"))
	require.ErrorIs(t, err, ErrKnowledgeNotFound)

	for _, query := range []string{"退款", "如何申请退款？", "err-042"} {
		matches, err := SearchKnowledge(ctx, 1, library.Id, query)
		require.NoError(t, err)
		require.NotEmpty(t, matches, query)
		require.Equal(t, document.Id, matches[0].DocumentId)
		require.Equal(t, "退款流程", matches[0].Heading)
		require.Equal(t, 1, matches[0].StartLine)
		require.Equal(t, 2, matches[0].EndLine)
	}
	hidden, err := SearchKnowledge(ctx, 2, library.Id, "退款")
	require.NoError(t, err)
	require.Empty(t, hidden)
	_, err = GetKnowledgeDocument(ctx, 2, document.Id)
	require.ErrorIs(t, err, ErrKnowledgeNotFound)
	_, err = ListKnowledgeDocuments(ctx, 2, library.Id)
	require.ErrorIs(t, err, ErrKnowledgeNotFound)
	preview, err := GetKnowledgeDocument(ctx, 1, document.Id)
	require.NoError(t, err)
	require.Equal(t, document.Body, preview.Body)
	require.ErrorIs(t, DeleteKnowledgeDocument(ctx, 2, library.Id, document.Id), ErrKnowledgeNotFound)
	require.ErrorIs(t, DeleteKnowledgeLibrary(ctx, 2, library.Id), ErrKnowledgeNotFound)

	for range 2 {
		session := ChatSession{UserId: 1, Model: "test-model"}
		require.NoError(t, CreateChatSession(&session))
		require.ErrorIs(t, SetChatKnowledgeLibrary(ctx, 2, session.Id, library.Id), ErrKnowledgeNotFound)
		require.NoError(t, SetChatKnowledgeLibrary(ctx, 1, session.Id, library.Id))
		actual, err := GetChatSessionById(session.Id, 1)
		require.NoError(t, err)
		require.Equal(t, library.Id, actual.KnowledgeLibraryId)
	}

	require.NoError(t, DeleteKnowledgeDocument(ctx, 1, library.Id, document.Id))
	matches, err := SearchKnowledge(ctx, 1, library.Id, "退款")
	require.NoError(t, err)
	require.Empty(t, matches)
	_, err = GetKnowledgeDocument(ctx, 1, document.Id)
	require.ErrorIs(t, err, ErrKnowledgeNotFound)
	libraries, err := ListKnowledgeLibraries(ctx, 1)
	require.NoError(t, err)
	require.Zero(t, libraries[0].Size)
	require.Zero(t, libraries[0].DocumentCount)

	require.NoError(t, DeleteKnowledgeLibrary(ctx, 1, library.Id))
	sessions, err := GetUserChatSessions(1)
	require.NoError(t, err)
	require.Len(t, sessions, 2)
	for _, session := range sessions {
		require.Zero(t, session.KnowledgeLibraryId)
	}
}

func TestKnowledgeImportLimitsAndSearchBound(t *testing.T) {
	previous := DB
	DB = openAgentRunTestDB(t)
	t.Cleanup(func() { DB = previous })
	require.NoError(t, DB.AutoMigrate(&KnowledgeLibrary{}, &KnowledgeDocument{}, &KnowledgeChunk{}))
	ctx := context.Background()
	library, err := CreateKnowledgeLibrary(ctx, 1, "容量验证")
	require.NoError(t, err)

	_, err = ImportKnowledgeDocument(ctx, 1, library.Id, "invalid.txt", []byte{0xff})
	require.Error(t, err)
	_, err = ImportKnowledgeDocument(ctx, 1, library.Id, "invalid.pdf", []byte("text"))
	require.Error(t, err)
	config.OptionMapRWMutex.Lock()
	oldOptions := config.OptionMap
	config.OptionMap = map[string]string{}
	config.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		config.OptionMapRWMutex.Lock()
		config.OptionMap = oldOptions
		config.OptionMapRWMutex.Unlock()
	})
	require.NoError(t, DB.AutoMigrate(&Option{}))
	require.Error(t, UpdateOption(knowledgeLimitsOption, `{"fileMiB":4,"libraryMiB":3}`))
	require.NoError(t, UpdateOption(knowledgeLimitsOption, `{"fileMiB":3,"libraryMiB":4}`))
	config.OptionMapRWMutex.Lock()
	delete(config.OptionMap, knowledgeLimitsOption)
	config.OptionMapRWMutex.Unlock()
	loadOptionsFromDatabase()
	limits := GetKnowledgeLimits()
	require.Equal(t, 3<<20, limits.FileBytes())
	require.Equal(t, 4<<20, limits.LibraryBytes())

	_, err = ImportKnowledgeDocument(ctx, 1, library.Id, "large.txt", make([]byte, limits.FileBytes()+1))
	require.ErrorIs(t, err, ErrKnowledgeFileTooLarge)
	original := knowledgeTestPNG(t)
	doc, err := ImportKnowledgeDocument(ctx, 1, library.Id, "large.png", original)
	require.NoError(t, err)
	require.Greater(t, doc.Size, 2<<20)
	metadata, text, err := GetKnowledgeDocumentPreview(ctx, 1, doc.Id)
	require.NoError(t, err)
	require.Equal(t, "image/png", metadata.ContentType)
	require.Empty(t, metadata.Body)
	require.Empty(t, text)
	download, err := GetKnowledgeDocument(ctx, 1, doc.Id)
	require.NoError(t, err)
	require.Equal(t, original, download.Body)
	_, err = GetKnowledgeDocument(ctx, 2, doc.Id)
	require.ErrorIs(t, err, ErrKnowledgeNotFound)
	body := []byte("ERR-042 " + strings.Repeat("x", 2<<20))
	_, err = ImportKnowledgeDocument(ctx, 1, library.Id, "overflow.txt", body)
	require.ErrorIs(t, err, ErrKnowledgeLimit)
	require.NoError(t, DeleteKnowledgeDocument(ctx, 1, library.Id, doc.Id))
	_, err = ImportKnowledgeDocument(ctx, 1, library.Id, "bounded.txt", body)
	require.NoError(t, err)
	require.NoError(t, UpdateOption(knowledgeLimitsOption, `{"fileMiB":1,"libraryMiB":1}`))
	_, err = ImportKnowledgeDocument(ctx, 1, library.Id, "overflow.txt", []byte("ERR-042"))
	require.ErrorIs(t, err, ErrKnowledgeLimit)
	matches, err := SearchKnowledge(ctx, 1, library.Id, "ERR-042")
	require.NoError(t, err)
	require.NotEmpty(t, matches)
}

func knowledgeTestPNG(t *testing.T) []byte {
	t.Helper()
	picture := image.NewRGBA(image.Rect(0, 0, 1000, 800))
	for offset := 3; offset < len(picture.Pix); offset += 4 {
		picture.Pix[offset] = 255
	}
	imageBytes := new(bytes.Buffer)
	require.NoError(t, (&png.Encoder{CompressionLevel: png.NoCompression}).Encode(imageBytes, picture))
	return imageBytes.Bytes()
}

func TestKnowledgeFormattedText(t *testing.T) {
	ctx := context.Background()
	for _, fixture := range []struct {
		ext  string
		xml  map[string]string
		text string
	}{
		{".docx", map[string]string{"word/document.xml": "<document><p><r><t>refund procedure</t></r></p></document>"}, "refund procedure"},
		{".xlsx", map[string]string{
			"xl/sharedStrings.xml":     "<sst><si><t>total</t></si></sst>",
			"xl/worksheets/sheet1.xml": `<worksheet><sheetData><row><c t="s"><v>0</v></c><c><v>7250</v></c></row></sheetData></worksheet>`,
		}, "total	7250"},
		{".pptx", map[string]string{"ppt/slides/slide1.xml": "<slide><p><t>launch schedule</t></p></slide>"}, "launch schedule"},
	} {
		t.Run(fixture.ext, func(t *testing.T) {
			var body bytes.Buffer
			archive := zip.NewWriter(&body)
			for name, xml := range fixture.xml {
				entry, err := archive.Create(name)
				require.NoError(t, err)
				_, err = entry.Write([]byte(xml))
				require.NoError(t, err)
			}
			require.NoError(t, archive.Close())
			got, err := knowledgeText(ctx, fixture.ext, body.Bytes())
			require.NoError(t, err)
			require.Contains(t, got, fixture.text)
		})
	}
}

func TestKnowledgePDFExtractsExistingText(t *testing.T) {
	if _, err := exec.LookPath("pdftotext"); err != nil {
		t.Skip("pdftotext not installed")
	}
	content := "BT /F1 12 Tf 0 25 Td (knowledge PDF fixture) Tj ET"
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Count 1 /Kids [3 0 R] >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 250 250] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
	}
	var pdf bytes.Buffer
	pdf.WriteString("%PDF-1.4\n")
	offsets := make([]int, 0, len(objects))
	for i, object := range objects {
		offsets = append(offsets, pdf.Len())
		fmt.Fprintf(&pdf, "%d 0 obj\n%s\nendobj\n", i+1, object)
	}
	xref := pdf.Len()
	fmt.Fprintf(&pdf, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, offset := range offsets {
		fmt.Fprintf(&pdf, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&pdf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	text, err := knowledgeText(context.Background(), ".pdf", pdf.Bytes())
	require.NoError(t, err)
	require.Contains(t, text, "knowledge PDF fixture")
}
