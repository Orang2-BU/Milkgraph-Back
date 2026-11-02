package master

import (
	"fmt"
)

type ChangeoverEvaluation struct {
	RuleID         string `json:"ruleId"`
	Precedence     string `json:"precedence"`
	Activity       string `json:"activity"`
	Condition      string `json:"condition"`
	RequiresCIP    bool   `json:"requiresCip"`
	CIPRuleID      string `json:"cipRuleId,omitempty"`
	CIPRequirement string `json:"cipRequirement,omitempty"`
	AdditionalNote string `json:"additionalNote,omitempty"`
}

func (c *Catalog) ClassifyChangeover(fromFinishedSKU, toFinishedSKU string) (ChangeoverEvaluation, error) {
	finished := byKey(c.Tables["finished_sku_catalog"], "finished_sku_id")
	products := byKey(c.Tables["product_catalog"], "sku_id")

	fRow, ok := finished[fromFinishedSKU]
	if !ok {
		return ChangeoverEvaluation{}, fmt.Errorf("unknown fromFinishedSKU: %q", fromFinishedSKU)
	}
	tRow, ok := finished[toFinishedSKU]
	if !ok {
		return ChangeoverEvaluation{}, fmt.Errorf("unknown toFinishedSKU: %q", toFinishedSKU)
	}

	pFrom, ok := products[fRow["liquid_sku_id"]]
	if !ok {
		return ChangeoverEvaluation{}, fmt.Errorf("unknown product: %q", fRow["liquid_sku_id"])
	}
	pTo, ok := products[tRow["liquid_sku_id"]]
	if !ok {
		return ChangeoverEvaluation{}, fmt.Errorf("unknown product: %q", tRow["liquid_sku_id"])
	}

	flavourFrom := pFrom["flavour"]
	flavourTo := pTo["flavour"]
	sizeFrom := fRow["pack_size_code"]
	sizeTo := tRow["pack_size_code"]
	marketFrom := fRow["market_variant"]
	marketTo := tRow["market_variant"]

	var eval ChangeoverEvaluation

	if flavourFrom != flavourTo {
		eval = ChangeoverEvaluation{
			RuleID:         "VARIANT",
			Precedence:     "FIRST",
			Activity:       "Variant Changeover",
			Condition:      "Flavour_variant_changes",
			AdditionalNote: "Override Size Change dan Paper Change; CIP tetap independen",
		}
		// Check CIP requirements based on cip_rules.csv
		if (flavourFrom == "Chocolate" && flavourTo == "Strawberry") || (flavourFrom == "Strawberry" && flavourTo == "Chocolate") {
			eval.RequiresCIP = true
			eval.CIPRuleID = "CIP_FLAVOUR"
			eval.CIPRequirement = "CIP wajib untuk Chocolate ke Strawberry dan Strawberry ke Chocolate"
		} else if (flavourFrom == "Chocolate" || flavourFrom == "Strawberry") && flavourTo == "Full Cream" {
			eval.RequiresCIP = true
			eval.CIPRuleID = "CIP_CH_ST"
			eval.CIPRequirement = "CIP selesai sebelum Full Cream diproses"
		}
		return eval, nil
	}

	if sizeFrom != sizeTo {
		eval = ChangeoverEvaluation{
			RuleID:         "SIZE",
			Precedence:     "SECOND",
			Activity:       "Size Change",
			Condition:      "Same_flavour_different_size_any_market",
			AdditionalNote: "Berlaku walau market berubah; override Paper Change; CIP tetap independen",
		}
		return eval, nil
	}

	if marketFrom != marketTo {
		eval = ChangeoverEvaluation{
			RuleID:         "PAPER",
			Precedence:     "THIRD",
			Activity:       "Paper Change",
			Condition:      "Different_market_variant_same_size_same_flavour",
			AdditionalNote: "Tetap cek End Production; End Storage Volume dan SOP transisi lain",
		}
		return eval, nil
	}

	return ChangeoverEvaluation{
		RuleID:         "NONE",
		Precedence:     "LAST",
		Activity:       "None",
		Condition:      "Same_market_variant_same_size_same_flavour",
		AdditionalNote: "Tetap tambahkan CIP bila trigger terpenuhi",
	}, nil
}

func (c *Catalog) IsLineFinishedEligible(lineID, finishedSKUID string) bool {
	for _, row := range c.Tables["line_finished_sku_compatibility"] {
		if row["line_id"] == lineID && row["finished_sku_id"] == finishedSKUID && row["eligible"] == "Y" {
			return true
		}
	}
	return false
}

func (c *Catalog) CheckAsepticTankCompatibility(lineA, lineB, skuA, skuB string) error {
	dedications := byKey(c.Tables["line_dedication"], "line_id")
	dedA, okA := dedications[lineA]
	dedB, okB := dedications[lineB]
	if !okA || !okB {
		return fmt.Errorf("unknown line: %s or %s", lineA, lineB)
	}

	tankA := dedA["source_aseptic_tank_id"]
	tankB := dedB["source_aseptic_tank_id"]

	if tankA == tankB && skuA != skuB {
		return fmt.Errorf("Aseptic Tank %s shared by %s and %s cannot receive different products (%s vs %s) simultaneously (AT_SAME_PRODUCT)", tankA, lineA, lineB, skuA, skuB)
	}
	return nil
}
