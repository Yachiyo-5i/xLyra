package backup

import (
	"archive/zip"
	"compress/flate"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"time"
)

const backupPayload = "zip-jsonl"

type manifest struct {
	FormatVersion int       `json:"format_version"`
	App           string    `json:"app"`
	Payload       string    `json:"payload"`
	CreatedAt     time.Time `json:"created_at"`
	Tables        []string  `json:"tables"`
}

type archivePayload struct {
	Manifest manifest
	Config   map[string]any
}

func writeArchive(payload archivePayload, writeDatabase func(*zip.Writer) error, dst io.Writer) error {
	zw := zip.NewWriter(dst)
	zw.RegisterCompressor(zip.Deflate, func(out io.Writer) (io.WriteCloser, error) {
		return flate.NewWriter(out, flate.BestSpeed)
	})

	if err := writeArchiveJSONFile(zw, "manifest.json", payload.Manifest); err != nil {
		return err
	}
	if err := writeArchiveJSONFile(zw, "config.json", payload.Config); err != nil {
		return err
	}
	if err := writeDatabase(zw); err != nil {
		_ = zw.Close()
		return err
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("close zip: %w", err)
	}
	return nil
}

func parseArchive(filename string) (archivePayload, databaseDump, error) {
	zr, err := zip.OpenReader(filename)
	if err != nil {
		return archivePayload{}, databaseDump{}, fmt.Errorf("open backup archive: %w", err)
	}
	defer zr.Close()

	var payload archivePayload
	if err := decodeArchiveJSON(zr.File, "manifest.json", &payload.Manifest); err != nil {
		return archivePayload{}, databaseDump{}, err
	}
	if err := decodeArchiveJSON(zr.File, "config.json", &payload.Config); err != nil {
		return archivePayload{}, databaseDump{}, err
	}

	dump := databaseDump{Tables: make(map[string][]map[string]any, len(exportTables)), archivePath: filename}
	for _, table := range backupTables {
		if _, streamed := streamedImportTables[table.Name]; streamed {
			// Large detail tables are streamed from the archive during import;
			// only verify the entry exists so validation still fails fast.
			if archiveFile(zr.File, path.Join("database", table.Name+".jsonl")) == nil {
				return archivePayload{}, databaseDump{}, fmt.Errorf("backup archive missing %s", path.Join("database", table.Name+".jsonl"))
			}
			continue
		}
		rows, err := decodeArchiveTable(zr.File, table.Name)
		if err != nil {
			return archivePayload{}, databaseDump{}, err
		}
		dump.Tables[table.Name] = rows
	}
	return payload, dump, nil
}

func writeArchiveJSONFile(zw *zip.Writer, name string, value any) error {
	w, err := zw.CreateHeader(&zip.FileHeader{
		Name:     name,
		Method:   zip.Deflate,
		Modified: time.Now().UTC(),
	})
	if err != nil {
		return fmt.Errorf("write zip header %s: %w", name, err)
	}
	encoder := json.NewEncoder(w)
	if err := encoder.Encode(value); err != nil {
		return fmt.Errorf("encode %s: %w", name, err)
	}
	return nil
}

func decodeArchiveJSON(files []*zip.File, name string, dst any) error {
	file := archiveFile(files, name)
	if file == nil {
		return fmt.Errorf("backup archive missing %s", name)
	}
	reader, err := file.Open()
	if err != nil {
		return fmt.Errorf("open %s: %w", name, err)
	}
	defer reader.Close()

	decoder := json.NewDecoder(reader)
	decoder.UseNumber()
	if err := decoder.Decode(dst); err != nil {
		return fmt.Errorf("decode %s: %w", name, err)
	}
	return nil
}

func decodeArchiveTable(files []*zip.File, table string) ([]map[string]any, error) {
	name := path.Join("database", table+".jsonl")
	file := archiveFile(files, name)
	if file == nil {
		return nil, fmt.Errorf("backup archive missing %s", name)
	}
	reader, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", name, err)
	}
	defer reader.Close()

	decoder := json.NewDecoder(reader)
	decoder.UseNumber()
	rows := []map[string]any{}
	for {
		var row map[string]any
		err := decoder.Decode(&row)
		if err == io.EOF {
			return rows, nil
		}
		if err != nil {
			return nil, fmt.Errorf("decode %s: %w", name, err)
		}
		rows = append(rows, row)
	}
}

func archiveFile(files []*zip.File, name string) *zip.File {
	for _, file := range files {
		if file.Name == name {
			return file
		}
	}
	return nil
}
