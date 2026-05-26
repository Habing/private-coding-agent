package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

func loadRecords(path, format string) ([]map[string]any, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	format = strings.ToLower(strings.TrimSpace(format))
	if format == "" {
		format = guessFormat(path)
	}
	switch format {
	case "json":
		return parseJSONArray(b)
	case "jsonl", "ndjson":
		return parseJSONL(b)
	case "csv":
		return parseCSV(b)
	default:
		return nil, fmt.Errorf("unsupported format %q", format)
	}
}

func guessFormat(path string) string {
	lower := strings.ToLower(path)
	switch {
	case strings.HasSuffix(lower, ".jsonl"), strings.HasSuffix(lower, ".ndjson"):
		return "jsonl"
	case strings.HasSuffix(lower, ".csv"):
		return "csv"
	default:
		return "json"
	}
}

func parseJSONArray(b []byte) ([]map[string]any, error) {
	var arr []map[string]any
	if err := json.Unmarshal(b, &arr); err == nil {
		return arr, nil
	}
	var wrap struct {
		Records []map[string]any `json:"records"`
	}
	if err := json.Unmarshal(b, &wrap); err == nil && wrap.Records != nil {
		return wrap.Records, nil
	}
	return nil, fmt.Errorf("expected JSON array or {records: [...]}")
}

func parseJSONL(b []byte) ([]map[string]any, error) {
	lines := bytes.Split(b, []byte("\n"))
	out := make([]map[string]any, 0, len(lines))
	for i, line := range lines {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal(line, &rec); err != nil {
			return nil, fmt.Errorf("jsonl line %d: %w", i+1, err)
		}
		out = append(out, rec)
	}
	return out, nil
}

func parseCSV(b []byte) ([]map[string]any, error) {
	r := csv.NewReader(bytes.NewReader(b))
	r.TrimLeadingSpace = true
	rows, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(rows) < 2 {
		return nil, fmt.Errorf("csv needs header + at least one row")
	}
	header := rows[0]
	out := make([]map[string]any, 0, len(rows)-1)
	for _, row := range rows[1:] {
		rec := make(map[string]any, len(header))
		for i, col := range header {
			val := ""
			if i < len(row) {
				val = row[i]
			}
			rec[col] = val
		}
		out = append(out, rec)
	}
	return out, nil
}

func writeRecords(path, format string, records []map[string]any) error {
	var payload []byte
	var err error
	switch strings.ToLower(format) {
	case "json", "":
		payload, err = json.MarshalIndent(map[string]any{"records": records}, "", "  ")
	case "jsonl", "ndjson":
		var buf bytes.Buffer
		for _, rec := range records {
			line, e := json.Marshal(rec)
			if e != nil {
				return e
			}
			buf.Write(line)
			buf.WriteByte('\n')
		}
		payload = buf.Bytes()
	default:
		return fmt.Errorf("write format %q not supported", format)
	}
	if err != nil {
		return err
	}
	return os.WriteFile(path, payload, 0o644)
}

func truncateRecords(records []map[string]any, max int) []map[string]any {
	if max <= 0 || len(records) <= max {
		return records
	}
	return records[:max]
}

func readFileLimit(path string, maxBytes int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if maxBytes <= 0 {
		return io.ReadAll(f)
	}
	return io.ReadAll(io.LimitReader(f, maxBytes))
}
