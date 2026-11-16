package macro

import (
	"strings"
	"testing"
)

// Parser unit tests for the 8 fetch clients — fixtures mirror the shapes MacroClientParseTest
// (Java) feeds the same parsers: gap markers, unit conversions, anchor cross-checks.

func TestParseFredResponse_SkipsGapMarkers(t *testing.T) {
	body := `{"observations":[` +
		`{"date":"2026-06-24","value":"7000000"},` +
		`{"date":"2026-07-01","value":"."},` +
		`{"date":"2026-07-08","value":""},` +
		`{"date":"bad-date","value":"123"}` +
		`]}`
	pts := parseFredResponse("WALCL", body)
	if len(pts) != 1 {
		t.Fatalf("expected 1 point (gap/blank/bad skipped), got %d", len(pts))
	}
	if got := pts[0].Date.Format("2006-01-02"); got != "2026-06-24" {
		t.Fatalf("expected 2026-06-24, got %s", got)
	}
	if pts[0].Value.String() != "7000000" {
		t.Fatalf("expected 7000000, got %s", pts[0].Value.String())
	}
}

func TestParseEcbCSV_TimePeriodAndObsValueColumns(t *testing.T) {
	header := "FREQ,ADJUSTMENT,REF_AREA,INDICATOR,_BREAKS,UNIT_MEASURE,UNIT_MULT,INSTRUMENT,SECTOR,COUNTERPART_SECTOR,COUNTERPART_AREA,CURRENCY_DENOM,TIME_PERIOD,OBS_VALUE,OBS_STATUS"
	row := "M,U2,Y,V,M20,X,1,U2,2300,Z01,E,EUR,2026-05,16000000.5,A"
	pts := parseEcbCSV(header + "\n" + row + "\n")
	if len(pts) != 1 {
		t.Fatalf("expected 1 point, got %d", len(pts))
	}
	if got := pts[0].Date.Format("2006-01-02"); got != "2026-05-01" {
		t.Fatalf("expected 2026-05-01, got %s", got)
	}
	if pts[0].Value.String() != "16000000.5" {
		t.Fatalf("expected 16000000.5, got %s", pts[0].Value.String())
	}
}

func TestParseBoeCSV_MonthStartNormalization(t *testing.T) {
	csv := "DATE,LPMVWYH\n\"31 May 2026\",1234567.8\n\"1 Jun 2026\",1234999\n"
	pts := parseBoeCSV(csv)
	if len(pts) != 2 {
		t.Fatalf("expected 2 points, got %d", len(pts))
	}
	if got := pts[0].Date.Format("2006-01-02"); got != "2026-05-01" {
		t.Fatalf("expected month-start 2026-05-01, got %s", got)
	}
	if got := pts[1].Date.Format("2006-01-02"); got != "2026-06-01" {
		t.Fatalf("expected month-start 2026-06-01, got %s", got)
	}
}

func TestParseBojCSV_HundredMillionConversionAndNullSkip(t *testing.T) {
	csv := "KEY,VALUE\nSERIES_CODE,NAME_OF_TIME_SERIES,SURVEY_DATES,VALUES\n" +
		"MAM1NAM2M2MO,Money stock (M2),202605,12000000\n" +
		"MAM1NAM2M2MO,Money stock (M2),202606,null\n"
	pts := parseBojCSV(csv)
	if len(pts) != 1 {
		t.Fatalf("expected 1 point (null month skipped), got %d", len(pts))
	}
	if got := pts[0].Date.Format("2006-01-02"); got != "2026-05-01" {
		t.Fatalf("expected 2026-05-01, got %s", got)
	}
	// Raw unit "100 million yen" ×100 → JPY millions.
	if pts[0].Value.String() != "1200000000" {
		t.Fatalf("expected 1200000000, got %s", pts[0].Value.String())
	}
}

func TestParseChinaDataLive_HundredMillionConversion(t *testing.T) {
	body := `{"success":true,"data":{"unit":"100 Million CNY","source":"PBOC","data":[` +
		`{"date":"2026-05","value":"3536700"},` +
		`{"date":"bad","value":"1"}` +
		`]}}`
	pts := parseChinaDataLive(body)
	if len(pts) != 1 {
		t.Fatalf("expected 1 point, got %d", len(pts))
	}
	if pts[0].Value.String() != "353670000" {
		t.Fatalf("expected 353670000 (×100), got %s", pts[0].Value.String())
	}
	if got := pts[0].Date.Format("2006-01-02"); got != "2026-05-01" {
		t.Fatalf("expected 2026-05-01, got %s", got)
	}
	if pts := parseChinaDataLive(`{"success":false}`); len(pts) != 0 {
		t.Fatalf("expected empty on success=false, got %d", len(pts))
	}
}

func TestParseTradingECHTML_TwoAnchorCrossCheck(t *testing.T) {
	good := `<script type="application/ld+json">{"temporalCoverage": "1996-01-31/2026-06-30"}</script>` +
		`<p>Money Supply M2 in China increased to 356709.76 CNY Billion in June from ...</p>`
	pts := parseTradingECHTML(good)
	if len(pts) != 1 {
		t.Fatalf("expected 1 tip point, got %d", len(pts))
	}
	if got := pts[0].Date.Format("2006-01-02"); got != "2026-06-01" {
		t.Fatalf("expected 2026-06-01, got %s", got)
	}
	// CNY Billion ×1000 → CNY millions.
	if pts[0].Value.String() != "356709760" {
		t.Fatalf("expected 356709760, got %s", pts[0].Value.String())
	}

	// Month mismatch between anchors → reject (stale-date-with-fresh-value guard).
	mismatch := strings.Replace(good, "in June", "in May", 1)
	if pts := parseTradingECHTML(mismatch); len(pts) != 0 {
		t.Fatalf("expected rejection on month mismatch, got %v", pts)
	}
	// Missing anchor → empty, never a misparse.
	if pts := parseTradingECHTML("<html>redesigned</html>"); len(pts) != 0 {
		t.Fatalf("expected empty on layout change, got %v", pts)
	}
}

func TestParseYChartsHTML_FullPrecisionObservation(t *testing.T) {
	html := `<script type="application/ld+json">{"@type":"Observation",` +
		`"value": 356710.84300 ,"unitText": "CNY Billion",` +
		`"observationDate": "2026-06-30" ,"priceCurrency": "CNY"}</script>`
	pts := parseYChartsHTML(html)
	if len(pts) != 1 {
		t.Fatalf("expected 1 tip point, got %d", len(pts))
	}
	if got := pts[0].Date.Format("2006-01-02"); got != "2026-06-01" {
		t.Fatalf("expected month-start 2026-06-01, got %s", got)
	}
	if pts[0].Value.String() != "356710843" {
		t.Fatalf("expected 356710843, got %s", pts[0].Value.String())
	}
}

func TestParsePbcReport_WanYiConversion(t *testing.T) {
	html := `<p>6月末，广义货币(M2)余额356.7万亿元，同比增长8.3%</p>`
	pts := parsePbcReport(html, "2026", "6")
	if len(pts) != 1 {
		t.Fatalf("expected 1 tip point, got %d", len(pts))
	}
	// 万亿元 ×1,000,000 → CNY millions.
	if pts[0].Value.String() != "356700000" {
		t.Fatalf("expected 356700000, got %s", pts[0].Value.String())
	}
	if got := pts[0].Date.Format("2006-01-02"); got != "2026-06-01" {
		t.Fatalf("expected 2026-06-01, got %s", got)
	}
}
