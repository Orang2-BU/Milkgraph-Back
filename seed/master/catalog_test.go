package master

import (
	"strings"
	"testing"
)

func TestEmbeddedCatalog(t *testing.T) {
	c, err := Load(Files)
	if err != nil {
		t.Fatal(err)
	}
	for table, count := range map[string]int{"asset_configuration": 73, "asset_connections": 118, "asset_units": 7, "changeover_rules": 4, "cip_rules": 5, "constraint_catalog": 21, "finished_sku_catalog": 44, "line_dedication": 10, "line_finished_sku_compatibility": 116, "line_product_compatibility": 58, "market_variants": 2, "pack_size_codes": 5, "product_catalog": 8} {
		if got := len(c.Tables[table]); got != count {
			t.Errorf("%s got %d, want %d", table, got, count)
		}
	}
	if c.Tables["asset_configuration"][0]["capacity_l"] != "X" {
		t.Fatal("X lost")
	}
	if c.Tables["asset_connections"][0]["via_asset_id"] != "" {
		t.Fatal("blank via lost")
	}
	for _, row := range c.Tables["asset_connections"] {
		if row["max_flow_lph"] != "X" {
			t.Fatal("unexpected measured max flow")
		}
	}
}

func TestLineFinishedWhitelistDoesNotInventCartesianEligibility(t *testing.T) {
	c, err := Load(Files)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range c.Tables["line_finished_sku_compatibility"] {
		if row["line_id"] == "FL-01" && row["liquid_sku_id"] == "ESL-ST" && row["pack_size_code"] == "ESL_320" {
			t.Fatal("line dedication is a summary, not a cartesian whitelist")
		}
	}
}

func TestBrokenReferenceRejected(t *testing.T) {
	c, err := Load(Files)
	if err != nil {
		t.Fatal(err)
	}
	c.Tables["asset_connections"][0]["source_asset_id"] = "missing"
	if err := c.Validate(); err == nil {
		t.Fatal("missing source accepted")
	}
}

func TestLineTankConnectionRejected(t *testing.T) {
	c, err := Load(Files)
	if err != nil {
		t.Fatal(err)
	}
	c.Tables["line_dedication"][0]["source_aseptic_tank_id"] = "AT-05"
	if err := c.Validate(); err == nil {
		t.Fatal("accepted line assigned to unconnected tank")
	}
}

func TestMissingFinishedSKUCompatibilityRejected(t *testing.T) {
	c, err := Load(Files)
	if err != nil {
		t.Fatal(err)
	}
	c.Tables["line_finished_sku_compatibility"] = c.Tables["line_finished_sku_compatibility"][1:]
	if err := c.Validate(); err == nil {
		t.Fatal("accepted incomplete compatibility whitelist")
	}
}

func TestFinishedSKUProjectionRejected(t *testing.T) {
	c, err := Load(Files)
	if err != nil {
		t.Fatal(err)
	}
	c.Tables["line_finished_sku_compatibility"][0]["liquid_sku_id"] = "UHT-SK"
	if err := c.Validate(); err == nil {
		t.Fatal("wrong SKU accepted")
	}
}

func TestCSVHeaderRejected(t *testing.T) {
	data, err := Files.ReadFile("pack_size_codes.csv")
	if err != nil {
		t.Fatal(err)
	}
	bad := strings.Replace(string(data), "exact_size_ml", "wrong_column", 1)
	if _, err := parse("pack_size_codes", schemas["pack_size_codes"], []byte(bad)); err == nil {
		t.Fatal("bad header accepted")
	}
}
