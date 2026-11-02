package master

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"io/fs"
	"strings"
)

var schemas = map[string][]string{
	"asset_configuration":             {"asset_id", "asset_type", "compatible_product_family", "capacity_l", "input_flow_lph", "output_flow_lph", "process_speed_lph", "holding_time_limit_hours", "cip_duration_minutes", "speed_packph", "node_role", "asset_name"},
	"asset_connections":               {"connection_id", "source_asset_id", "destination_asset_id", "via_asset_id", "max_flow_lph", "flow_type", "routing_condition"},
	"asset_units":                     {"field", "applies_to", "unit", "definition"},
	"changeover_rules":                {"rule_id", "precedence", "condition", "activity", "duration_minutes", "additional_note"},
	"cip_rules":                       {"rule_id", "trigger", "asset_scope", "requirement", "duration_minutes"},
	"constraint_catalog":              {"rule_id", "scope", "description"},
	"finished_sku_catalog":            {"finished_sku_id", "liquid_sku_id", "pack_size_code", "market_variant"},
	"line_dedication":                 {"line_id", "allowed_sku_ids", "pack_size_codes", "source_aseptic_tank_id", "speed_packph"},
	"line_finished_sku_compatibility": {"line_id", "finished_sku_id", "liquid_sku_id", "pack_size_code", "market_variant", "eligible"},
	"line_product_compatibility":      {"line_id", "sku_id", "pack_size_code", "eligible"},
	"market_variants":                 {"market_variant", "market_name", "applicable_liquid_skus"},
	"pack_size_codes":                 {"pack_size_code", "description", "exact_size_ml"},
	"product_catalog":                 {"sku_id", "product_name", "product_family", "flavour", "allowed_pack_size_codes", "market_variants"},
}

type Row map[string]string

type Catalog struct {
	Tables map[string][]Row
}

func Load(fsys fs.FS) (*Catalog, error) {
	catalog := &Catalog{Tables: make(map[string][]Row, len(schemas))}
	for name, headers := range schemas {
		data, err := fs.ReadFile(fsys, name+".csv")
		if err != nil {
			return nil, fmt.Errorf("read %s.csv: %w", name, err)
		}
		rows, err := parse(name, headers, data)
		if err != nil {
			return nil, err
		}
		catalog.Tables[name] = rows
	}
	if err := catalog.Validate(); err != nil {
		return nil, err
	}
	return catalog, nil
}

func parse(name string, headers []string, data []byte) ([]Row, error) {
	reader := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})))
	reader.FieldsPerRecord = len(headers)
	actual, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("read %s header: %w", name, err)
	}
	if strings.Join(actual, "\x00") != strings.Join(headers, "\x00") {
		return nil, fmt.Errorf("unexpected %s header", name)
	}
	var rows []Row
	for rowNumber := 2; ; rowNumber++ {
		values, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read %s row %d: %w", name, rowNumber, err)
		}
		row := make(Row, len(headers))
		for i, header := range headers {
			row[header] = strings.TrimSpace(values[i])
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("%s has no rows", name)
	}
	return rows, nil
}

func (c *Catalog) Summary() map[string]any {
	counts := make(map[string]int, len(c.Tables))
	for name, rows := range c.Tables {
		counts[name] = len(rows)
	}
	return map[string]any{
		"tables":                 counts,
		"totalTables":            len(c.Tables),
		"status":                 "structurally_valid",
		"numericalPlanningReady": false,
		"version":                "v2_1",
		"source":                 "Greenfields_Ideation_Package_v2_1",
	}
}

func (c *Catalog) Assets() []Row { return c.Tables["asset_configuration"] }

func (c *Catalog) Connections() []Row { return c.Tables["asset_connections"] }

// Topology returns allowed static routes. It is deliberately not a material-transfer ledger.
func (c *Catalog) Topology() map[string]any {
	return map[string]any{
		"kind":  "route_capability",
		"nodes": c.Assets(),
		"edges": c.Connections(),
	}
}

func (c *Catalog) Products() map[string]any {
	return map[string]any{
		"products":       c.Tables["product_catalog"],
		"packSizes":      c.Tables["pack_size_codes"],
		"marketVariants": c.Tables["market_variants"],
	}
}

func (c *Catalog) FinishedSKUs() []Row { return c.Tables["finished_sku_catalog"] }

func (c *Catalog) Lines() map[string]any {
	return map[string]any{
		"dedications":           c.Tables["line_dedication"],
		"productCompatibility":  c.Tables["line_product_compatibility"],
		"finishedCompatibility": c.Tables["line_finished_sku_compatibility"],
	}
}

func (c *Catalog) Rules() map[string]any {
	return map[string]any{
		"changeoverRules":   c.Tables["changeover_rules"],
		"cipRules":          c.Tables["cip_rules"],
		"constraintCatalog": c.Tables["constraint_catalog"],
		"assetUnits":        c.Tables["asset_units"],
	}
}

func (c *Catalog) Validate() error {
	if len(c.Tables) != len(schemas) {
		return fmt.Errorf("expected %d master tables, got %d", len(schemas), len(c.Tables))
	}
	assets, err := c.keySet("asset_configuration", "asset_id")
	if err != nil {
		return err
	}
	products, err := c.keySet("product_catalog", "sku_id")
	if err != nil {
		return err
	}
	packs, err := c.keySet("pack_size_codes", "pack_size_code")
	if err != nil {
		return err
	}
	markets, err := c.keySet("market_variants", "market_variant")
	if err != nil {
		return err
	}
	finished, err := c.keySet("finished_sku_catalog", "finished_sku_id")
	if err != nil {
		return err
	}
	lines, err := c.keySet("line_dedication", "line_id")
	if err != nil {
		return err
	}
	if err := c.unique("asset_units", "field"); err != nil {
		return err
	}
	if err := c.unique("asset_connections", "connection_id"); err != nil {
		return err
	}
	if err := c.unique("changeover_rules", "rule_id"); err != nil {
		return err
	}
	if err := c.unique("cip_rules", "rule_id"); err != nil {
		return err
	}
	if err := c.unique("constraint_catalog", "rule_id"); err != nil {
		return err
	}
	if err := c.uniqueComposite("line_product_compatibility", "line_id", "sku_id", "pack_size_code"); err != nil {
		return err
	}
	if err := c.uniqueComposite("line_finished_sku_compatibility", "line_id", "finished_sku_id"); err != nil {
		return err
	}

	assetRows := byKey(c.Tables["asset_configuration"], "asset_id")
	lineTankRoutes := map[string]struct{}{}
	for i, row := range c.Tables["asset_connections"] {
		if err := referenced("asset_connections", i+2, "source_asset_id", row["source_asset_id"], assets); err != nil {
			return err
		}
		if err := referenced("asset_connections", i+2, "destination_asset_id", row["destination_asset_id"], assets); err != nil {
			return err
		}
		if row["flow_type"] == "aseptic_supply" {
			lineTankRoutes[tuple(row["source_asset_id"], row["destination_asset_id"])] = struct{}{}
		}
		if row["via_asset_id"] != "" {
			if err := referenced("asset_connections", i+2, "via_asset_id", row["via_asset_id"], assets); err != nil {
				return err
			}
			if assetRows[row["via_asset_id"]]["asset_type"] != "Dedicated Pipe" {
				return fmt.Errorf("asset_connections row %d: via_asset_id must be a Dedicated Pipe", i+2)
			}
		}
	}
	for i, row := range c.Tables["product_catalog"] {
		if err := referencesList("product_catalog", i+2, "allowed_pack_size_codes", row["allowed_pack_size_codes"], packs); err != nil {
			return err
		}
		if err := referencesList("product_catalog", i+2, "market_variants", row["market_variants"], markets); err != nil {
			return err
		}
	}
	finishedRows := byKey(c.Tables["finished_sku_catalog"], "finished_sku_id")
	for i, row := range c.Tables["finished_sku_catalog"] {
		if err := referenced("finished_sku_catalog", i+2, "liquid_sku_id", row["liquid_sku_id"], products); err != nil {
			return err
		}
		if err := referenced("finished_sku_catalog", i+2, "pack_size_code", row["pack_size_code"], packs); err != nil {
			return err
		}
		if err := referenced("finished_sku_catalog", i+2, "market_variant", row["market_variant"], markets); err != nil {
			return err
		}
		product := byKey(c.Tables["product_catalog"], "sku_id")[row["liquid_sku_id"]]
		if !contains(split(product["allowed_pack_size_codes"]), row["pack_size_code"]) {
			return fmt.Errorf("finished_sku_catalog row %d: pack_size_code is not allowed by product", i+2)
		}
		if !contains(split(product["market_variants"]), row["market_variant"]) {
			return fmt.Errorf("finished_sku_catalog row %d: market_variant is not allowed by product", i+2)
		}
	}
	for i, row := range c.Tables["line_dedication"] {
		if err := referenced("line_dedication", i+2, "line_id", row["line_id"], assets); err != nil {
			return err
		}
		if assetRows[row["line_id"]]["asset_type"] != "Filling Line" {
			return fmt.Errorf("line_dedication row %d: line_id must be a Filling Line", i+2)
		}
		if err := referenced("line_dedication", i+2, "source_aseptic_tank_id", row["source_aseptic_tank_id"], assets); err != nil {
			return err
		}
		if assetRows[row["source_aseptic_tank_id"]]["asset_type"] != "Aseptic Tank" {
			return fmt.Errorf("line_dedication row %d: source_aseptic_tank_id must be an Aseptic Tank", i+2)
		}
		if _, ok := lineTankRoutes[tuple(row["source_aseptic_tank_id"], row["line_id"])]; !ok {
			return fmt.Errorf("line_dedication row %d: no aseptic_supply connection", i+2)
		}
		if err := referencesList("line_dedication", i+2, "allowed_sku_ids", row["allowed_sku_ids"], products); err != nil {
			return err
		}
		if err := referencesList("line_dedication", i+2, "pack_size_codes", row["pack_size_codes"], packs); err != nil {
			return err
		}
	}
	lineProducts := make(map[string]struct{})
	for i, row := range c.Tables["line_product_compatibility"] {
		if err := referenced("line_product_compatibility", i+2, "line_id", row["line_id"], lines); err != nil {
			return err
		}
		if err := referenced("line_product_compatibility", i+2, "sku_id", row["sku_id"], products); err != nil {
			return err
		}
		if err := referenced("line_product_compatibility", i+2, "pack_size_code", row["pack_size_code"], packs); err != nil {
			return err
		}
		dedication := byKey(c.Tables["line_dedication"], "line_id")[row["line_id"]]
		if !contains(split(dedication["allowed_sku_ids"]), row["sku_id"]) || !contains(split(dedication["pack_size_codes"]), row["pack_size_code"]) {
			return fmt.Errorf("line_product_compatibility row %d: outside line dedication", i+2)
		}
		lineProducts[tuple(row["line_id"], row["sku_id"], row["pack_size_code"])] = struct{}{}
	}
	expectedFinished := map[string]struct{}{}
	for key := range lineProducts {
		parts := strings.Split(key, "\x00")
		for _, sku := range c.Tables["finished_sku_catalog"] {
			if sku["liquid_sku_id"] == parts[1] && sku["pack_size_code"] == parts[2] {
				expectedFinished[tuple(parts[0], sku["finished_sku_id"])] = struct{}{}
			}
		}
	}
	for i, row := range c.Tables["line_finished_sku_compatibility"] {
		if err := referenced("line_finished_sku_compatibility", i+2, "line_id", row["line_id"], lines); err != nil {
			return err
		}
		if err := referenced("line_finished_sku_compatibility", i+2, "finished_sku_id", row["finished_sku_id"], finished); err != nil {
			return err
		}
		if err := referenced("line_finished_sku_compatibility", i+2, "liquid_sku_id", row["liquid_sku_id"], products); err != nil {
			return err
		}
		if err := referenced("line_finished_sku_compatibility", i+2, "pack_size_code", row["pack_size_code"], packs); err != nil {
			return err
		}
		if err := referenced("line_finished_sku_compatibility", i+2, "market_variant", row["market_variant"], markets); err != nil {
			return err
		}
		catalog := finishedRows[row["finished_sku_id"]]
		if catalog["liquid_sku_id"] != row["liquid_sku_id"] || catalog["pack_size_code"] != row["pack_size_code"] || catalog["market_variant"] != row["market_variant"] {
			return fmt.Errorf("line_finished_sku_compatibility row %d: finished SKU projection mismatch", i+2)
		}
		if _, ok := lineProducts[tuple(row["line_id"], row["liquid_sku_id"], row["pack_size_code"])]; !ok {
			return fmt.Errorf("line_finished_sku_compatibility row %d: missing liquid SKU compatibility", i+2)
		}
	}
	if len(c.Tables["line_finished_sku_compatibility"]) != len(expectedFinished) {
		return fmt.Errorf("line_finished_sku_compatibility count mismatch: got %d, want %d", len(c.Tables["line_finished_sku_compatibility"]), len(expectedFinished))
	}
	return nil
}

func (c *Catalog) keySet(table, key string) (map[string]struct{}, error) {
	if err := c.unique(table, key); err != nil {
		return nil, err
	}
	set := make(map[string]struct{}, len(c.Tables[table]))
	for _, row := range c.Tables[table] {
		set[row[key]] = struct{}{}
	}
	return set, nil
}
func (c *Catalog) unique(table, key string) error {
	seen := map[string]struct{}{}
	for i, row := range c.Tables[table] {
		value := row[key]
		if value == "" {
			return fmt.Errorf("%s row %d: %s is required", table, i+2, key)
		}
		if _, exists := seen[value]; exists {
			return fmt.Errorf("%s row %d: duplicate %s", table, i+2, key)
		}
		seen[value] = struct{}{}
	}
	return nil
}
func (c *Catalog) uniqueComposite(table string, keys ...string) error {
	seen := map[string]struct{}{}
	for i, row := range c.Tables[table] {
		values := make([]string, len(keys))
		for j, key := range keys {
			if row[key] == "" {
				return fmt.Errorf("%s row %d: %s is required", table, i+2, key)
			}
			values[j] = row[key]
		}
		key := tuple(values...)
		if _, exists := seen[key]; exists {
			return fmt.Errorf("%s row %d: duplicate relation", table, i+2)
		}
		seen[key] = struct{}{}
	}
	return nil
}
func referenced(table string, row int, field, value string, set map[string]struct{}) error {
	if value == "" {
		return fmt.Errorf("%s row %d: %s is required", table, row, field)
	}
	if _, ok := set[value]; !ok {
		return fmt.Errorf("%s row %d: unknown %s %q", table, row, field, value)
	}
	return nil
}
func referencesList(table string, row int, field, value string, set map[string]struct{}) error {
	items := split(value)
	if len(items) == 0 {
		return fmt.Errorf("%s row %d: %s is required", table, row, field)
	}
	for _, item := range items {
		if err := referenced(table, row, field, item, set); err != nil {
			return err
		}
	}
	return nil
}
func byKey(rows []Row, key string) map[string]Row {
	out := make(map[string]Row, len(rows))
	for _, row := range rows {
		out[row[key]] = row
	}
	return out
}
func split(value string) []string {
	parts := strings.Split(value, ";")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
func contains(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}
func tuple(values ...string) string { return strings.Join(values, "\x00") }
