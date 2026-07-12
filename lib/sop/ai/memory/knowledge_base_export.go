package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/sharedcode/sop"
)

type ImportJSONOptions struct {
	BootstrapCategorizerIfMissing bool
}

// ExportData	defines	the	structure	of	the	KnowledgeBase	JSON	payload.
type ExportData[T any] struct {
	Config            *KnowledgeBaseConfig `json:"config,omitempty"`
	CategorizerBundle *CategorizerBundle   `json:"categorizer_bundle,omitempty"`
	Categories        []*Category          `json:"categories"`
	Documents         []*Document          `json:"documents,omitempty"`
	Items             []ExportItem[T]      `json:"items"`
}

// ExportItem	dictates	what	fields	from	the	item	are	serialized.
type ExportItem[T any] struct {
	CategoryPath     string      `json:"category"`
	DocID            DocIDs      `json:"doc_id,omitempty"`
	Data             T           `json:"data"`
	Summaries        []string    `json:"summaries,omitempty"`
	SummariesVectors [][]float32 `json:"summaries_vectors,omitempty"`
	Positions        []VectorKey `json:"positions,omitempty"`
	VectorHash       string      `json:"vector_hash,omitempty"`
}

// ExportJSON	serializes	the	KnowledgeBase	contents	into	a	JSON	stream.
func (kb *KnowledgeBase[T]) ExportJSON(ctx context.Context, writer io.Writer) error {
	catBtree, err := kb.Store.Categories(ctx)
	if err != nil {
		return err
	}

	encoder := json.NewEncoder(writer)
	io.WriteString(writer, "{\n")

	//	1.	Config	block
	itemsBtree, err := kb.Store.Items(ctx)
	if err != nil {
		return err
	}
	hasConfig := false
	ok, _ := itemsBtree.First(ctx)
	for ok {
		item, err := itemsBtree.GetCurrentValue(ctx)
		if err == nil && item.ID == sop.NilUUID {
			b, err := json.Marshal(item.Data)
			if err == nil {
				var cfg KnowledgeBaseConfig
				if json.Unmarshal(b, &cfg) == nil {
					io.WriteString(writer, "\"config\":	")
					encoder.Encode(cfg)
					hasConfig = true
				}
			}
			break
		}
		ok, _ = itemsBtree.Next(ctx)
	}

	if hasConfig {
		io.WriteString(writer, ",\n")
	}

	cfg, err := kb.GetConfig(ctx)
	if err != nil {
		return err
	}
	if cfg != nil && cfg.CategorizerConfig != nil {
		bundle := &CategorizerBundle{
			ID:      strings.TrimSpace(cfg.CategorizerRef),
			Version: "1",
			Kind:    strings.TrimSpace(cfg.CategorizerConfig.Type),
			Config:  cfg.CategorizerConfig,
			Source:  "embedded",
		}
		if bundle.ID == "" {
			bundle.ID = fmt.Sprintf("%s-categorizer", kb.Store.Name())
		}
		if bundle.Kind == "" {
			bundle.Kind = "rule"
		}
		io.WriteString(writer, "\"categorizer_bundle\":")
		encoder.Encode(bundle)
		io.WriteString(writer, ",\n")
	}

	//	2.	Categories	array
	io.WriteString(writer, "\"categories\":	[\n")
	catMap := make(map[sop.UUID]string)
	firstCat := true
	ok, _ = catBtree.First(ctx)
	for ok {
		cat, err := catBtree.GetCurrentValue(ctx)
		if err == nil && cat != nil {
			if !firstCat {
				io.WriteString(writer, ",\n")
			}
			firstCat = false
			encoder.Encode(cat)
			catMap[cat.ID] = cat.Name
		}
		ok, _ = catBtree.Next(ctx)
	}
	io.WriteString(writer, "\n]")

	//	3.	Items	array
	io.WriteString(writer, ",\n\"items\":	[\n")
	firstItem := true
	ok, _ = itemsBtree.First(ctx)
	for ok {
		item, err := itemsBtree.GetCurrentValue(ctx)
		if err == nil && item.ID != sop.NilUUID {
			var vectors [][]float32
			if len(item.Positions) > 0 {
				vecBtree, _ := kb.Store.Vectors(ctx)
				if vecBtree != nil {
					for _, pos := range item.Positions {
						has, _ := vecBtree.Find(ctx, pos, false)
						if has {
							v, _ := vecBtree.GetCurrentValue(ctx)
							vectors = append(vectors, v.Data)
						}
					}
				}
			}

			catName := catMap[item.CategoryID]
			if !firstItem {
				io.WriteString(writer, ",\n")
			}
			firstItem = false
			encoder.Encode(ExportItem[T]{
				CategoryPath:     catName,
				DocID:            item.DocID,
				Data:             item.Data,
				Summaries:        item.Summaries,
				SummariesVectors: vectors,
				Positions:        item.Positions,
				VectorHash:       item.VectorHash,
			})
		}
		ok, _ = itemsBtree.Next(ctx)
	}

	io.WriteString(writer, "\n]\n}\n")
	return nil
}

func buildBootstrapCategorizerConfig(categories []Category) *CategorizerConfig {
	if len(categories) == 0 {
		return nil
	}
	seenPaths := make(map[string]struct{}, len(categories))
	entries := make([]any, 0, len(categories))
	for _, cat := range categories {
		path := strings.TrimSpace(cat.Path)
		name := strings.TrimSpace(cat.Name)
		if path == "" {
			path = name
		}
		if path == "" {
			continue
		}
		if _, ok := seenPaths[path]; ok {
			continue
		}
		seenPaths[path] = struct{}{}

		keywords := make([]string, 0, 4)
		for _, part := range strings.Split(path, "/") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			keywords = append(keywords, strings.ToLower(part))
		}
		if name != "" {
			keywords = append(keywords, strings.ToLower(name))
		}
		if len(keywords) == 0 {
			continue
		}
		sanitized := make([]string, 0, len(keywords))
		seenKeywords := make(map[string]struct{}, len(keywords))
		for _, kw := range keywords {
			if kw == "" {
				continue
			}
			if _, ok := seenKeywords[kw]; ok {
				continue
			}
			seenKeywords[kw] = struct{}{}
			sanitized = append(sanitized, kw)
		}
		entries = append(entries, map[string]any{"path": path, "keywords": sanitized})
	}
	if len(entries) == 0 {
		return nil
	}
	return &CategorizerConfig{Type: "rule", Default: map[string]any{"categories": entries}, Fallback: "uncategorized"}
}

// ImportJSON	deserializes	a	JSON	stream	and	ingests	it	into	the	KnowledgeBase.
func (kb *KnowledgeBase[T]) ImportJSON(ctx context.Context, reader io.Reader, persona string, onEnrich ...func(*ExportItem[T])) error {
	return kb.ImportJSONWithOptions(ctx, reader, persona, ImportJSONOptions{}, onEnrich...)
}

func (kb *KnowledgeBase[T]) ImportJSONWithOptions(ctx context.Context, reader io.Reader, persona string, opts ImportJSONOptions, onEnrich ...func(*ExportItem[T])) error {
	decoder := json.NewDecoder(reader)

	//	Read	opening	'{'
	t, err := decoder.Token()
	if err != nil {
		return err
	}
	if delim, ok := t.(json.Delim); !ok || delim != '{' {
		return fmt.Errorf("expected	'{'	at	the	beginning	of	stream")
	}

	catBtree, err := kb.Store.Categories(ctx)
	if err != nil {
		return err
	}
	catsByPath, err := kb.Store.CategoriesByPath(ctx)
	if err != nil {
		return err
	}
	uuidMap := make(map[sop.UUID]sop.UUID)

	hasMissingVectors := false
	importedCategories := make([]Category, 0)

	for decoder.More() {
		t, err := decoder.Token()
		if err != nil {
			return err
		}
		key, ok := t.(string)
		if !ok {
			return fmt.Errorf("expected	string	key")
		}

		if key == "config" {
			var configData KnowledgeBaseConfig
			if err := decoder.Decode(&configData); err != nil {
				return fmt.Errorf("decode kb config: %w", err)
			}
			configData.Description = strings.TrimSpace(configData.Description)
			if err := kb.SetConfig(ctx, &configData); err != nil {
				return fmt.Errorf("persist kb config: %w", err)
			}
			continue
		}
		if key == "categorizer_bundle" {
			var bundle CategorizerBundle
			if err := decoder.Decode(&bundle); err != nil {
				return fmt.Errorf("decode categorizer bundle: %w", err)
			}
			if cfg, err := kb.GetConfig(ctx); err == nil && cfg != nil {
				if bundle.ID != "" {
					cfg.CategorizerRef = bundle.ID
				}
				if bundle.Config != nil {
					cfg.CategorizerConfig = bundle.Config
				}
				if err := kb.SetConfig(ctx, cfg); err != nil {
					return fmt.Errorf("restore categorizer bundle config: %w", err)
				}
			}
			continue
		}
		if key == "categories" {
			//	Read	opening	'['
			if _, err := decoder.Token(); err != nil { //	'['
				return err
			}
			for decoder.More() {
				var c Category
				if err := decoder.Decode(&c); err != nil {
					return err
				}
				importedCategories = append(importedCategories, c)
				if len(c.CenterVector) == 0 {
					hasMissingVectors = true
				}
				//	Deduplicate	based	on	Path	instead	of	Name
				ok, _ := catBtree.First(ctx)
				found := false
				for ok {
					existing, _ := catBtree.GetCurrentValue(ctx)
					if existing != nil && existing.Path == c.Path && c.Path != "" {
						uuidMap[c.ID] = existing.ID
						found = true
						break
					} else if existing != nil && existing.Path == "" && existing.Name == c.Name {
						uuidMap[c.ID] = existing.ID
						found = true
						break
					}
					ok, _ = catBtree.Next(ctx)
				}
				if !found {
					oldID := c.ID
					if c.ID.IsNil() {
						c.ID = sop.NewUUID()
					}
					uuidMap[oldID] = c.ID
					for i := range c.ParentIDs {
						if mappedID, ok := uuidMap[c.ParentIDs[i].ParentID]; ok {
							c.ParentIDs[i].ParentID = mappedID
						}
					}
					catBtree.Add(ctx, c.ID, &c)
					path := c.Path
					if path == "" {
						path = c.Name
					}
					if path != "" {
						catsByPath.Add(ctx, path, c.ID)
					}
				}
			} // Rebuild Bi-Directional Children Links
			updates := make(map[sop.UUID][]sop.UUID)
			okCat, _ := catBtree.First(ctx)
			for okCat {
				c, _ := catBtree.GetCurrentValue(ctx)
				if c != nil {
					for _, p := range c.ParentIDs {
						updates[p.ParentID] = append(updates[p.ParentID], c.ID)
					}
				}
				okCat, _ = catBtree.Next(ctx)
			}
			for parentID, children := range updates {
				if found, _ := catBtree.Find(ctx, parentID, false); found {
					parent, _ := catBtree.GetCurrentValue(ctx)
					if parent != nil {
						for _, newChild := range children {
							exists := false
							for _, existingChild := range parent.ChildrenIDs {
								if existingChild == newChild {
									exists = true
									break
								}
							}
							if !exists {
								parent.ChildrenIDs = append(parent.ChildrenIDs, newChild)
							}
						}
						catBtree.UpdateCurrentItem(ctx, parentID, parent)
					}
				}
			}
			if _, err := decoder.Token(); err != nil { // Read closing ']'
				return err
			}
		} else if key == "documents" {
			// Read opening '['
			if _, err := decoder.Token(); err != nil { // '['
				return err
			}
			for decoder.More() {
				var doc Document
				if err := decoder.Decode(&doc); err != nil {
					return err
				}
				if doc.ID.IsNil() {
					doc.ID = sop.NewUUID()
				}
				if err := kb.Store.UpsertDocument(ctx, doc); err != nil {
					return err
				}
			}
			if _, err := decoder.Token(); err != nil { // Read closing ']'
				return err
			}
		} else if key == "items" {
			//	Read	opening	'['
			if _, err := decoder.Token(); err != nil { //	'['
				return err
			}
			var thoughts []Thought[T]
			for decoder.More() {
				var it ExportItem[T]
				if err := decoder.Decode(&it); err != nil {
					return err
				}

				if len(it.SummariesVectors) == 0 {
					hasMissingVectors = true
				}

				for _, enrich := range onEnrich {
					if enrich != nil {
						enrich(&it)
					}
				}

				// If DocumentMode is true, we truncate the generic text/description metadata
				// after enrichment (so embeddings/summaries are generated fairly), but before storage.
				if cfg, err := kb.GetConfig(ctx); err == nil && cfg != nil && cfg.DocumentMode {
					if dataMap, ok := any(it.Data).(map[string]any); ok {
						if txt, ok := dataMap["text"].(string); ok && len(txt) > 800 {
							runes := []rune(txt)
							if len(runes) > 800 {
								dataMap["text"] = string(runes[:800]) + "... (truncated)"
							}
						}
						if desc, ok := dataMap["description"].(string); ok && len(desc) > 800 {
							runes := []rune(desc)
							if len(runes) > 800 {
								dataMap["description"] = string(runes[:800]) + "... (truncated)"
							}
						}
						it.Data = any(dataMap).(T)
					}
				}

				if parsedID, err := sop.ParseUUID(it.CategoryPath); err == nil {
					if mapped, ok := uuidMap[parsedID]; ok {
						it.CategoryPath = mapped.String()
					}
				}
				thoughts = append(thoughts, Thought[T]{
					CategoryPath: it.CategoryPath,
					DocID:        it.DocID,
					Data:         it.Data,
					Vectors:      it.SummariesVectors,
					Summaries:    it.Summaries,
					Positions:    it.Positions,
					VectorHash:   it.VectorHash,
				})

				//	Submit	in	batches	to	keep	memory	usage	low
				if len(thoughts) >= 500 {
					if err := kb.IngestThoughts(ctx, thoughts, persona); err != nil {
						return err
					}
					thoughts = thoughts[:0] //	Keep	capacity,	reset	length
				}
			}
			if _, err := decoder.Token(); err != nil { //	Read	closing	']'
				return err
			}

			//	Submit	remaining	tail
			if len(thoughts) > 0 {
				if err := kb.IngestThoughts(ctx, thoughts, persona); err != nil {
					return err
				}
			}
		} else {
			//	Ignore	any	unknown	root	keys
			var discard any
			decoder.Decode(&discard)
		}
	}

	if _, err := decoder.Token(); err != nil { //	Read	closing	'}'
		return err
	}
	if cfg, err := kb.GetConfig(ctx); err == nil && cfg != nil {
		if opts.BootstrapCategorizerIfMissing && cfg.CategorizerConfig == nil && strings.TrimSpace(cfg.CategorizerRef) == "" && len(importedCategories) > 0 {
			if generated := buildBootstrapCategorizerConfig(importedCategories); generated != nil {
				cfg.CategorizerConfig = generated
				if err := kb.SetConfig(ctx, cfg); err != nil {
					return fmt.Errorf("bootstrap categorizer config: %w", err)
				}
			}
		}
		now := time.Now().Unix()
		cfg.LastModified = now
		if !hasMissingVectors {
			cfg.LastVectorized = now
		}
		if err := kb.SetConfig(ctx, cfg); err != nil {
			return err
		}
	}
	return nil
}
