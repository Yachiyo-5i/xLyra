package backup

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"time"
)

const (
	backupPayload        = "zip-jsonl"
	archiveRowCountsFile = "row_counts.json"
)

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

func writeArchive(payload archivePayload, writeDatabase func(*zip.Writer) (map[string]int, error), dst io.Writer) error {
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
	rowCounts, err := writeDatabase(zw)
	if err != nil {
		_ = zw.Close()
		return err
	}
	if err := writeArchiveJSONFile(zw, archiveRowCountsFile, rowCounts); err != nil {
		_ = zw.Close()
		return err
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("close zip: %w", err)
	}
	return nil
}

func parseArchive(filename string) (archivePayload, databaseDump, error) {
	return parseArchiveContext(context.Background(), filename)
}

func parseArchiveContext(ctx context.Context, filename string) (archivePayload, databaseDump, error) {
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
	totalRows, err := archiveTotalRows(ctx, zr.File, dump.Tables)
	if err != nil {
		return archivePayload{}, databaseDump{}, err
	}
	dump.TotalRows = totalRows
	return payload, dump, nil
}

func archiveTotalRows(ctx context.Context, files []*zip.File, loaded map[string][]map[string]any) (int, error) {
	if archiveFile(files, archiveRowCountsFile) != nil {
		rowCounts := map[string]int{}
		if err := decodeArchiveJSON(files, archiveRowCountsFile, &rowCounts); err != nil {
			return 0, err
		}
		total := 0
		for _, table := range backupTables {
			rows, ok := rowCounts[table.Name]
			if !ok || rows < 0 {
				return 0, fmt.Errorf("invalid row count for %s", table.Name)
			}
			total += rows
		}
		return total, nil
	}
	total := 0
	for _, table := range backupTables {
		if _, streamed := streamedImportTables[table.Name]; !streamed {
			total += len(loaded[table.Name])
			continue
		}
		name := path.Join("database", table.Name+".jsonl")
		file := archiveFile(files, name)
		rows, err := countArchiveRowsContext(ctx, file)
		if err != nil {
			return 0, fmt.Errorf("count %s: %w", name, err)
		}
		total += rows
	}
	return total, nil
}

func countArchiveRows(file *zip.File) (int, error) {
	return countArchiveRowsContext(context.Background(), file)
}

func countArchiveRowsContext(ctx context.Context, file *zip.File) (int, error) {
	reader, err := file.Open()
	if err != nil {
		return 0, err
	}
	defer reader.Close()

	buffer := make([]byte, 128<<10)
	newline := []byte{'\n'}
	rows := 0
	readAny := false
	last := byte('\n')
	for {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		n, readErr := reader.Read(buffer)
		if n > 0 {
			readAny = true
			last = buffer[n-1]
			rows += bytes.Count(buffer[:n], newline)
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return 0, readErr
		}
	}
	if readAny && last != '\n' {
		rows++
	}
	return rows, nil
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
