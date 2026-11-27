package model

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"os/exec"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const knowledgeIndexBytes = 1 << 20
const knowledgeOfficeXMLBytes = 16 << 20

var knowledgeFormats = map[string]string{
	".txt":  "text/plain; charset=utf-8",
	".md":   "text/markdown; charset=utf-8",
	".csv":  "text/csv; charset=utf-8",
	".tsv":  "text/tab-separated-values; charset=utf-8",
	".pdf":  "application/pdf",
	".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	".pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".png":  "image/png",
	".gif":  "image/gif",
	".webp": "image/webp",
}

func KnowledgeExtensions() []string {
	extensions := make([]string, 0, len(knowledgeFormats))
	for extension := range knowledgeFormats {
		extensions = append(extensions, extension)
	}
	slices.Sort(extensions)
	return extensions
}

func knowledgeText(ctx context.Context, extension string, body []byte) (string, error) {
	if len(body) == 0 {
		return "", ErrKnowledgeInvalidDocument
	}
	contentType := knowledgeFormats[extension]
	switch {
	case strings.HasPrefix(contentType, "image/"):
		if http.DetectContentType(body) != contentType {
			return "", ErrKnowledgeInvalidDocument
		}
		return "", nil
	case strings.HasPrefix(contentType, "text/"):
		if !utf8.Valid(body) || bytes.IndexByte(body, 0) >= 0 || len(bytes.TrimSpace(body)) == 0 {
			return "", ErrKnowledgeInvalidDocument
		}
		text := &knowledgeTextBuffer{}
		_, _ = text.Write(body)
		return text.String(), nil
	case extension == ".pdf":
		if !bytes.HasPrefix(body, []byte("%PDF-")) {
			return "", ErrKnowledgeInvalidDocument
		}
		return extractKnowledgePDF(ctx, body)
	default:
		return extractKnowledgeOffice(ctx, extension, body)
	}
}

type knowledgeTextBuffer struct{ bytes.Buffer }

func (text *knowledgeTextBuffer) Write(p []byte) (int, error) {
	n := min(max(0, knowledgeIndexBytes-text.Len()), len(p))
	_, _ = text.Buffer.Write(p[:n])
	return len(p), nil
}

func (text *knowledgeTextBuffer) String() string {
	return strings.ToValidUTF8(text.Buffer.String(), "")
}

func extractKnowledgePDF(ctx context.Context, body []byte) (string, error) {
	binary, err := exec.LookPath("pdftotext")
	if err != nil {
		return "", nil
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, "-enc", "UTF-8", "-", "-")
	command.Stdin = bytes.NewReader(body)
	text := &knowledgeTextBuffer{}
	command.Stdout = text
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", ErrKnowledgeInvalidDocument
	}
	return text.String(), nil
}

func extractKnowledgeOffice(ctx context.Context, extension string, body []byte) (string, error) {
	archive, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return "", ErrKnowledgeInvalidDocument
	}
	var entries []*zip.File
	var remaining uint64 = knowledgeOfficeXMLBytes
	for _, file := range archive.File {
		dir, base := path.Split(file.Name)
		relevant := extension == ".docx" && file.Name == "word/document.xml" ||
			extension == ".xlsx" && (file.Name == "xl/sharedStrings.xml" ||
				dir == "xl/worksheets/" && strings.HasPrefix(base, "sheet") && strings.HasSuffix(base, ".xml")) ||
			extension == ".pptx" && dir == "ppt/slides/" && strings.HasPrefix(base, "slide") && strings.HasSuffix(base, ".xml")
		if relevant {
			if file.UncompressedSize64 > remaining {
				return "", ErrKnowledgeInvalidDocument
			}
			remaining -= file.UncompressedSize64
			entries = append(entries, file)
		}
	}
	if len(entries) == 0 {
		return "", ErrKnowledgeInvalidDocument
	}
	slices.SortFunc(entries, func(a, b *zip.File) int {
		if a.Name == b.Name {
			return 0
		}
		if a.Name == "xl/sharedStrings.xml" {
			return -1
		}
		if b.Name == "xl/sharedStrings.xml" {
			return 1
		}
		if len(a.Name) != len(b.Name) {
			return len(a.Name) - len(b.Name)
		}
		return strings.Compare(a.Name, b.Name)
	})
	text := &knowledgeTextBuffer{}
	var sharedStrings []string
	for _, file := range entries {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		var err error
		if extension == ".xlsx" {
			err = extractKnowledgeSpreadsheet(text, file, &sharedStrings)
		} else {
			err = extractKnowledgeXML(text, file)
		}
		if err != nil {
			return "", ErrKnowledgeInvalidDocument
		}
	}
	return text.String(), nil
}

func extractKnowledgeSpreadsheet(text *knowledgeTextBuffer, file *zip.File, sharedStrings *[]string) error {
	input, err := file.Open()
	if err != nil {
		return err
	}
	defer input.Close()
	decoder := xml.NewDecoder(input)
	sharedTable := file.Name == "xl/sharedStrings.xml"
	var cell strings.Builder
	var cellType string
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		switch tag := token.(type) {
		case xml.StartElement:
			switch tag.Name.Local {
			case "c", "si":
				cell.Reset()
				cellType = ""
				for _, attr := range tag.Attr {
					if attr.Name.Local == "t" {
						cellType = attr.Value
					}
				}
			case "v", "t":
				var value string
				if err := decoder.DecodeElement(&value, &tag); err != nil {
					return err
				}
				cell.WriteString(value)
			}
		case xml.EndElement:
			switch tag.Name.Local {
			case "si":
				if sharedTable {
					*sharedStrings = append(*sharedStrings, cell.String())
				}
			case "c":
				value := cell.String()
				if cellType == "s" && !sharedTable {
					index, err := strconv.Atoi(value)
					if err != nil || index < 0 || index >= len(*sharedStrings) {
						return ErrKnowledgeInvalidDocument
					}
					value = (*sharedStrings)[index]
				}
				_, _ = text.Write([]byte(value))
				_, _ = text.Write([]byte("\t"))
			case "row":
				_, _ = text.Write([]byte("\n"))
			}
		}
	}
}

func extractKnowledgeXML(text *knowledgeTextBuffer, file *zip.File) error {
	input, err := file.Open()
	if err != nil {
		return err
	}
	defer input.Close()
	decoder := xml.NewDecoder(io.LimitReader(input, int64(file.UncompressedSize64)+1))
	var insideText bool
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		switch tag := token.(type) {
		case xml.StartElement:
			insideText = tag.Name.Local == "t"
		case xml.CharData:
			if insideText {
				_, _ = text.Write(tag)
			}
		case xml.EndElement:
			insideText = false
			if tag.Name.Local == "p" || tag.Name.Local == "si" || tag.Name.Local == "c" {
				_, _ = text.Write([]byte("\n"))
			}
		}
	}
}
