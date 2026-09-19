//go:build genapi

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type swaggerDoc struct {
	Info                 swaggerInfo                 `json:"info"`
	Host                 string                      `json:"host"`
	BasePath             string                      `json:"basePath"`
	Paths                map[string]map[string]op    `json:"paths"`
	Definitions          map[string]schema           `json:"definitions"`
	SecurityDefinitions  map[string]securityScheme   `json:"securityDefinitions"`
}

type swaggerInfo struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Version     string `json:"version"`
}

type op struct {
	Tags        []string               `json:"tags"`
	Summary     string                 `json:"summary"`
	Description string                 `json:"description"`
	Consumes    []string               `json:"consumes"`
	Produces    []string               `json:"produces"`
	Parameters  []param                `json:"parameters"`
	Responses   map[string]response    `json:"responses"`
	Security    []map[string][]string  `json:"security"`
}

type param struct {
	Name        string `json:"name"`
	In          string `json:"in"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
	Type        string `json:"type"`
	Schema      schema `json:"schema"`
}

type response struct {
	Description string `json:"description"`
	Schema      schema `json:"schema"`
}

type schema struct {
	Ref                  string            `json:"$ref"`
	Type                 string            `json:"type"`
	Format               string            `json:"format"`
	Description          string            `json:"description"`
	Example              any               `json:"example"`
	Properties           map[string]schema `json:"properties"`
	Items                *schema           `json:"items"`
	AdditionalProperties json.RawMessage   `json:"additionalProperties"`
	Required             []string          `json:"required"`
}

type securityScheme struct {
	Type        string `json:"type"`
	Name        string `json:"name"`
	In          string `json:"in"`
	Description string `json:"description"`
}

type endpoint struct {
	tag    string
	method string
	path   string
	op     op
}

func main() {
	root := findRoot()
	inPath := filepath.Join(root, "website", "static", "swagger.json")
	outPath := filepath.Join(root, "website", "content", "api.md")

	raw, err := os.ReadFile(inPath)
	if err != nil {
		fatal(fmt.Errorf("read swagger.json (run swag first): %w", err))
	}
	var doc swaggerDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		fatal(err)
	}

	md := render(doc)
	if err := os.WriteFile(outPath, []byte(md), 0o644); err != nil {
		fatal(err)
	}
	fmt.Printf("wrote %s\n", outPath)
}

func render(doc swaggerDoc) string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("title: API\n")
	b.WriteString("weight: 20\n")
	b.WriteString("---\n\n")

	desc := strings.TrimSpace(doc.Info.Description)
	if desc != "" {
		b.WriteString(desc)
		b.WriteString("\n\n")
	} else {
		b.WriteString("Control-plane JSON API for PotatoNetwork.\n\n")
	}

	if doc.Host != "" {
		fmt.Fprintf(&b, "Default host in the generated OpenAPI spec: `%s` (base path `%s`).\n\n", doc.Host, orSlash(doc.BasePath))
	}

	endpoints := collectEndpoints(doc)
	tags := tagOrder(endpoints)

	b.WriteString("## Endpoints\n\n")
	for _, tag := range tags {
		fmt.Fprintf(&b, "### %s\n\n", tag)
		for _, ep := range endpoints {
			if ep.tag != tag {
				continue
			}
			fmt.Fprintf(&b, "#### `%s %s`\n\n", strings.ToUpper(ep.method), ep.path)
			if ep.op.Summary != "" {
				b.WriteString(ep.op.Summary)
				b.WriteString("\n\n")
			}
			if d := strings.TrimSpace(ep.op.Description); d != "" && d != ep.op.Summary {
				b.WriteString(d)
				b.WriteString("\n\n")
			}
			if requiresAuth(ep.op) {
				b.WriteString("Auth: optional Bearer when `POTATONETWORK_API_TOKEN` is set.\n\n")
			}
			if len(ep.op.Consumes) > 0 {
				fmt.Fprintf(&b, "Consumes: `%s`\n\n", strings.Join(ep.op.Consumes, "`, `"))
			}
			if len(ep.op.Produces) > 0 {
				fmt.Fprintf(&b, "Produces: `%s`\n\n", strings.Join(ep.op.Produces, "`, `"))
			}
			writeParams(&b, ep.op.Parameters)
			writeResponses(&b, ep.op.Responses)
		}
	}

	if len(doc.Definitions) > 0 {
		b.WriteString("## Schemas\n\n")
		names := make([]string, 0, len(doc.Definitions))
		for name := range doc.Definitions {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			fmt.Fprintf(&b, "### `%s`\n\n", shortName(name))
			writeSchema(&b, doc.Definitions[name])
		}
	}

	return b.String()
}

func collectEndpoints(doc swaggerDoc) []endpoint {
	paths := make([]string, 0, len(doc.Paths))
	for p := range doc.Paths {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	methods := []string{"get", "post", "put", "patch", "delete", "head", "options"}
	var out []endpoint
	for _, path := range paths {
		ops := doc.Paths[path]
		for _, method := range methods {
			o, ok := ops[method]
			if !ok {
				continue
			}
			tag := "other"
			if len(o.Tags) > 0 && o.Tags[0] != "" {
				tag = o.Tags[0]
			}
			out = append(out, endpoint{tag: tag, method: method, path: path, op: o})
		}
	}
	return out
}

func tagOrder(endpoints []endpoint) []string {
	preferred := []string{"system", "profile", "baseline", "catalog", "ca", "rules", "stats"}
	seen := map[string]struct{}{}
	var order []string
	for _, t := range preferred {
		for _, ep := range endpoints {
			if ep.tag == t {
				if _, ok := seen[t]; !ok {
					order = append(order, t)
					seen[t] = struct{}{}
				}
				break
			}
		}
	}
	extras := make([]string, 0)
	for _, ep := range endpoints {
		if _, ok := seen[ep.tag]; !ok {
			extras = append(extras, ep.tag)
			seen[ep.tag] = struct{}{}
		}
	}
	sort.Strings(extras)
	return append(order, extras...)
}

func writeParams(b *strings.Builder, params []param) {
	if len(params) == 0 {
		return
	}
	b.WriteString("| Param | In | Required | Type | Description |\n")
	b.WriteString("| --- | --- | --- | --- | --- |\n")
	for _, p := range params {
		req := "no"
		if p.Required {
			req = "yes"
		}
		typ := schemaType(p.Schema)
		if typ == "" {
			typ = p.Type
		}
		if typ == "" {
			typ = "—"
		}
		fmt.Fprintf(b, "| `%s` | %s | %s | %s | %s |\n",
			p.Name, p.In, req, typ, escapeCell(p.Description))
	}
	b.WriteString("\n")
}

func writeResponses(b *strings.Builder, responses map[string]response) {
	if len(responses) == 0 {
		return
	}
	codes := make([]string, 0, len(responses))
	for code := range responses {
		codes = append(codes, code)
	}
	sort.Slice(codes, func(i, j int) bool {
		return codes[i] < codes[j]
	})
	b.WriteString("| Status | Schema | Description |\n")
	b.WriteString("| --- | --- | --- |\n")
	for _, code := range codes {
		r := responses[code]
		typ := schemaType(r.Schema)
		if typ == "" {
			typ = "—"
		}
		fmt.Fprintf(b, "| `%s` | %s | %s |\n", code, typ, escapeCell(r.Description))
	}
	b.WriteString("\n")
}

func writeSchema(b *strings.Builder, s schema) {
	if s.Type != "" && s.Type != "object" {
		fmt.Fprintf(b, "Type: `%s`", s.Type)
		if s.Format != "" {
			fmt.Fprintf(b, " (`%s`)", s.Format)
		}
		b.WriteString("\n\n")
	}
	if len(s.Properties) == 0 {
		if t := schemaType(s); t != "" && s.Type == "" {
			fmt.Fprintf(b, "Type: %s\n\n", t)
		}
		return
	}
	req := map[string]struct{}{}
	for _, r := range s.Required {
		req[r] = struct{}{}
	}
	names := make([]string, 0, len(s.Properties))
	for name := range s.Properties {
		names = append(names, name)
	}
	sort.Strings(names)
	b.WriteString("| Field | Type | Required | Notes |\n")
	b.WriteString("| --- | --- | --- | --- |\n")
	for _, name := range names {
		p := s.Properties[name]
		reqLabel := "no"
		if _, ok := req[name]; ok {
			reqLabel = "yes"
		}
		notes := p.Description
		if notes == "" && p.Example != nil {
			notes = fmt.Sprintf("example: `%v`", p.Example)
		} else if p.Example != nil && notes != "" {
			notes = fmt.Sprintf("%s; example: `%v`", notes, p.Example)
		}
		fmt.Fprintf(b, "| `%s` | %s | %s | %s |\n",
			name, schemaType(p), reqLabel, escapeCell(notes))
	}
	b.WriteString("\n")
}

func schemaType(s schema) string {
	if s.Ref != "" {
		return fmt.Sprintf("`%s`", shortName(s.Ref))
	}
	switch s.Type {
	case "array":
		if s.Items == nil {
			return "`array`"
		}
		inner := schemaType(*s.Items)
		inner = strings.Trim(inner, "`")
		return fmt.Sprintf("`array<%s>`", inner)
	case "object":
		if len(s.AdditionalProperties) > 0 && string(s.AdditionalProperties) != "false" {
			var nested schema
			if err := json.Unmarshal(s.AdditionalProperties, &nested); err == nil {
				inner := schemaType(nested)
				if inner != "" {
					inner = strings.Trim(inner, "`")
					return fmt.Sprintf("`map<string, %s>`", inner)
				}
			}
			var flag bool
			if err := json.Unmarshal(s.AdditionalProperties, &flag); err == nil && flag {
				return "`object`"
			}
		}
		if len(s.Properties) > 0 {
			return "`object`"
		}
		return "`object`"
	case "":
		return ""
	default:
		if s.Format != "" {
			return fmt.Sprintf("`%s(%s)`", s.Type, s.Format)
		}
		return fmt.Sprintf("`%s`", s.Type)
	}
}

func shortName(ref string) string {
	ref = strings.TrimPrefix(ref, "#/definitions/")
	if i := strings.LastIndex(ref, "."); i >= 0 && i+1 < len(ref) {
		return ref[i+1:]
	}
	return ref
}

func requiresAuth(o op) bool {
	for _, entry := range o.Security {
		if _, ok := entry["BearerAuth"]; ok {
			return true
		}
	}
	return false
}

func escapeCell(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.TrimSpace(s)
}

func orSlash(s string) string {
	if s == "" {
		return "/"
	}
	return s
}

func findRoot() string {
	wd, err := os.Getwd()
	if err != nil {
		fatal(err)
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return wd
		}
		dir = parent
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
