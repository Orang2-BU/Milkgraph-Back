package master

import "testing"

func TestClassifyChangeover(t *testing.T) {
	c, err := Load(Files)
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name     string
		from     string
		to       string
		wantRule string
		wantAct  string
		wantCIP  bool
	}{
		{
			name:     "flavour change FC to CH",
			from:     "UHT-FC-1000-LOCAL",
			to:       "UHT-CH-1000-LOCAL",
			wantRule: "VARIANT",
			wantAct:  "Variant Changeover",
			wantCIP:  false,
		},
		{
			name:     "flavour change CH to ST (CIP mandatory)",
			from:     "UHT-CH-1000-LOCAL",
			to:       "UHT-ST-1000-LOCAL",
			wantRule: "VARIANT",
			wantAct:  "Variant Changeover",
			wantCIP:  true,
		},
		{
			name:     "flavour change ST to FC (CIP mandatory)",
			from:     "UHT-ST-1000-LOCAL",
			to:       "UHT-FC-1000-LOCAL",
			wantRule: "VARIANT",
			wantAct:  "Variant Changeover",
			wantCIP:  true,
		},
		{
			name:     "size change same flavour any market",
			from:     "UHT-FC-1000-LOCAL",
			to:       "UHT-FC-950-EXP",
			wantRule: "SIZE",
			wantAct:  "Size Change",
			wantCIP:  false,
		},
		{
			name:     "paper change different market same size same flavour",
			from:     "UHT-FC-1000-LOCAL",
			to:       "UHT-FC-1000-EXP",
			wantRule: "PAPER",
			wantAct:  "Paper Change",
			wantCIP:  false,
		},
		{
			name:     "none identical sku",
			from:     "UHT-FC-1000-LOCAL",
			to:       "UHT-FC-1000-LOCAL",
			wantRule: "NONE",
			wantAct:  "None",
			wantCIP:  false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := c.ClassifyChangeover(tc.from, tc.to)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res.RuleID != tc.wantRule || res.Activity != tc.wantAct || res.RequiresCIP != tc.wantCIP {
				t.Fatalf("got rule=%s act=%s cip=%v, want rule=%s act=%s cip=%v",
					res.RuleID, res.Activity, res.RequiresCIP, tc.wantRule, tc.wantAct, tc.wantCIP)
			}
		})
	}
}

func TestLineCompatibilityCheck(t *testing.T) {
	c, err := Load(Files)
	if err != nil {
		t.Fatal(err)
	}

	// FL-01 is dedicated to ESL, should accept ESL-FC-1000-LOCAL and reject UHT-FC-1000-LOCAL
	if !c.IsLineFinishedEligible("FL-01", "ESL-FC-1000-LOCAL") {
		t.Fatal("expected FL-01 eligible for ESL-FC-1000-LOCAL")
	}
	if c.IsLineFinishedEligible("FL-01", "UHT-FC-1000-LOCAL") {
		t.Fatal("expected FL-01 NOT eligible for UHT-FC-1000-LOCAL")
	}

	// Check AT_SAME_PRODUCT constraint
	// FL-01 and FL-02 share AT-01
	if err := c.CheckAsepticTankCompatibility("FL-01", "FL-02", "ESL-FC", "ESL-CH"); err == nil {
		t.Fatal("expected error when shared AT-01 runs different products simultaneously")
	}
	if err := c.CheckAsepticTankCompatibility("FL-01", "FL-02", "ESL-FC", "ESL-FC"); err != nil {
		t.Fatalf("expected valid when shared AT-01 runs same product: %v", err)
	}
}
