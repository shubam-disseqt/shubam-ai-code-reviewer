// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package metrics_dashboard

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"time"
)

// Totals aggregates every run into a single scoreboard row rendered at the
// top of the dashboard.
type Totals struct {
	Runs             int
	TotalCostUSD     float64
	TotalFindings    int
	AvgDurationMs    int64
	TotalNewFindings int
}

// Report is the render input for the HTML template. TrafficBuckets is a
// 3-element histogram: green (low effort), yellow, red (high effort). If
// effort_score is not present in the log we fall back to bucketing by cost
// so the chart still shows a signal.
type Report struct {
	Runs           []RunMetrics
	Totals         Totals
	TrafficBuckets [3]int
	// RunsJSON is a pre-marshalled JSON payload injected into the page so
	// Chart.js can read it without a network fetch.
	RunsJSON    template.JS
	TrafficJSON template.JS
	Generated   time.Time
}

// Build turns a slice of runs into a Report. Empty input still returns a
// valid (all-zero) Report so the caller can always render something.
func Build(runs []RunMetrics) Report {
	rep := Report{Runs: runs, Generated: time.Now()}
	rep.Totals.Runs = len(runs)
	var totalDur int64
	for _, r := range runs {
		rep.Totals.TotalCostUSD += r.TotalCostUSD
		rep.Totals.TotalFindings += r.NewFindings + r.CarriedFindings
		rep.Totals.TotalNewFindings += r.NewFindings
		totalDur += r.DurationMs
		rep.TrafficBuckets[bucketFor(r)]++
	}
	if len(runs) > 0 {
		rep.Totals.AvgDurationMs = totalDur / int64(len(runs))
	}
	// Serialise for Chart.js. Errors are impossible on this shape but we
	// swallow rather than propagate — a marshalling bug would make the
	// dashboard blank, not the CLI fail.
	if b, err := json.Marshal(runs); err == nil {
		rep.RunsJSON = template.JS(b)
	} else {
		rep.RunsJSON = "[]"
	}
	if b, err := json.Marshal(rep.TrafficBuckets); err == nil {
		rep.TrafficJSON = template.JS(b)
	} else {
		rep.TrafficJSON = "[0,0,0]"
	}
	return rep
}

// bucketFor returns 0 (green), 1 (yellow), 2 (red). Prefers effort_score
// if the log carried it, else falls back to cost tiers picked to match a
// typical PR review (<$0.20 cheap, <$1 medium, else expensive).
// ponytail: fixed cost tiers, wire to policy if teams want tuning.
func bucketFor(r RunMetrics) int {
	if r.EffortScore > 0 {
		switch {
		case r.EffortScore <= 3:
			return 0
		case r.EffortScore <= 6:
			return 1
		default:
			return 2
		}
	}
	switch {
	case r.TotalCostUSD < 0.20:
		return 0
	case r.TotalCostUSD < 1.0:
		return 1
	default:
		return 2
	}
}

// Render writes the HTML dashboard to w. Chart.js is pulled from a CDN so
// the file stays a single self-contained artefact — no local asset bundle.
func Render(w io.Writer, rep Report) error {
	t, err := template.New("dashboard").Funcs(template.FuncMap{
		"fmtCost": func(f float64) string { return fmt.Sprintf("$%.2f", f) },
		"fmtMs": func(ms int64) string {
			if ms < 1000 {
				return fmt.Sprintf("%dms", ms)
			}
			return fmt.Sprintf("%.1fs", float64(ms)/1000.0)
		},
		"fmtTime": func(t time.Time) string { return t.Format("2006-01-02 15:04") },
		"last20": func(runs []RunMetrics) []RunMetrics {
			if len(runs) <= 20 {
				return runs
			}
			return runs[len(runs)-20:]
		},
	}).Parse(dashboardTmpl)
	if err != nil {
		return fmt.Errorf("parse template: %w", err)
	}
	if err := t.Execute(w, rep); err != nil {
		return fmt.Errorf("render template: %w", err)
	}
	return nil
}

const dashboardTmpl = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>zreview metrics</title>
<meta name="viewport" content="width=device-width,initial-scale=1">
<script src="https://cdn.jsdelivr.net/npm/chart.js@4.4.0/dist/chart.umd.min.js"></script>
<style>
  :root {
    --bg: #0f1115; --panel: #1a1d24; --text: #e6e8ee; --muted: #8a92a6;
    --accent: #7cc4ff; --green: #4ade80; --yellow: #fbbf24; --red: #f87171;
    --border: #262a33;
  }
  * { box-sizing: border-box; }
  body { margin: 0; font: 14px/1.5 -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
    background: var(--bg); color: var(--text); }
  header { padding: 24px 32px; border-bottom: 1px solid var(--border); }
  header h1 { margin: 0; font-size: 20px; font-weight: 600; letter-spacing: -0.01em; }
  header .sub { color: var(--muted); font-size: 12px; margin-top: 4px; }
  main { padding: 24px 32px; max-width: 1200px; margin: 0 auto; }
  .kpis { display: grid; grid-template-columns: repeat(auto-fit,minmax(180px,1fr));
    gap: 12px; margin-bottom: 24px; }
  .kpi { background: var(--panel); border: 1px solid var(--border); border-radius: 8px;
    padding: 16px; }
  .kpi .label { color: var(--muted); font-size: 11px; text-transform: uppercase;
    letter-spacing: 0.05em; }
  .kpi .value { font-size: 24px; font-weight: 600; margin-top: 4px; }
  .charts { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; margin-bottom: 24px; }
  .chart-panel { background: var(--panel); border: 1px solid var(--border); border-radius: 8px;
    padding: 16px; }
  .chart-panel h2 { margin: 0 0 12px 0; font-size: 13px; font-weight: 600; color: var(--muted);
    text-transform: uppercase; letter-spacing: 0.05em; }
  canvas { max-height: 280px; }
  table { width: 100%; border-collapse: collapse; background: var(--panel);
    border: 1px solid var(--border); border-radius: 8px; overflow: hidden; }
  th, td { padding: 10px 12px; text-align: left; border-bottom: 1px solid var(--border);
    font-variant-numeric: tabular-nums; }
  th { color: var(--muted); font-size: 11px; text-transform: uppercase; font-weight: 600;
    letter-spacing: 0.05em; }
  tr:last-child td { border-bottom: none; }
  td.num { text-align: right; }
  .empty { color: var(--muted); text-align: center; padding: 48px; }
  @media (max-width: 720px) { .charts { grid-template-columns: 1fr; } }
</style>
</head>
<body>
<header>
  <h1>zreview metrics</h1>
  <div class="sub">Generated {{fmtTime .Generated}} — {{.Totals.Runs}} run(s)</div>
</header>
<main>
{{if eq .Totals.Runs 0}}
  <div class="empty">No sessions found. Run <code>zreview review</code> to populate ~/.zreview/sessions.</div>
{{else}}
  <section class="kpis">
    <div class="kpi"><div class="label">Total runs</div><div class="value">{{.Totals.Runs}}</div></div>
    <div class="kpi"><div class="label">Total cost</div><div class="value">{{fmtCost .Totals.TotalCostUSD}}</div></div>
    <div class="kpi"><div class="label">New findings</div><div class="value">{{.Totals.TotalNewFindings}}</div></div>
    <div class="kpi"><div class="label">Avg duration</div><div class="value">{{fmtMs .Totals.AvgDurationMs}}</div></div>
  </section>

  <section class="charts">
    <div class="chart-panel"><h2>Cost per run (USD)</h2><canvas id="cost"></canvas></div>
    <div class="chart-panel"><h2>Findings per run</h2><canvas id="findings"></canvas></div>
  </section>

  <section class="charts">
    <div class="chart-panel"><h2>Traffic-light distribution</h2><canvas id="traffic"></canvas></div>
    <div class="chart-panel">
      <h2>Last 20 runs</h2>
      <table>
        <thead><tr><th>When</th><th>Files</th><th class="num">Cost</th><th class="num">Duration</th><th class="num">New</th><th class="num">Carried</th><th class="num">Resolved</th></tr></thead>
        <tbody>
        {{range last20 .Runs}}
          <tr>
            <td>{{fmtTime .Timestamp}}</td>
            <td>{{.FilesReviewed}}</td>
            <td class="num">{{fmtCost .TotalCostUSD}}</td>
            <td class="num">{{fmtMs .DurationMs}}</td>
            <td class="num">{{.NewFindings}}</td>
            <td class="num">{{.CarriedFindings}}</td>
            <td class="num">{{.ResolvedFindings}}</td>
          </tr>
        {{end}}
        </tbody>
      </table>
    </div>
  </section>

<script>
  const runs = {{.RunsJSON}};
  const traffic = {{.TrafficJSON}};
  const labels = runs.map(r => new Date(r.timestamp).toLocaleString());
  const opts = { responsive: true, maintainAspectRatio: false,
    scales: { x: { ticks: { color: '#8a92a6' } }, y: { ticks: { color: '#8a92a6' } } },
    plugins: { legend: { labels: { color: '#e6e8ee' } } } };

  new Chart(document.getElementById('cost'), {
    type: 'line',
    data: { labels, datasets: [{ label: 'USD', data: runs.map(r => r.total_cost_usd),
      borderColor: '#7cc4ff', backgroundColor: 'rgba(124,196,255,0.15)', fill: true, tension: 0.25 }] },
    options: opts
  });

  new Chart(document.getElementById('findings'), {
    type: 'line',
    data: { labels, datasets: [
      { label: 'new', data: runs.map(r => r.new_findings), borderColor: '#f87171', backgroundColor: 'rgba(248,113,113,0.4)', fill: true, tension: 0.25 },
      { label: 'carried', data: runs.map(r => r.carried_findings), borderColor: '#fbbf24', backgroundColor: 'rgba(251,191,36,0.4)', fill: true, tension: 0.25 },
      { label: 'resolved', data: runs.map(r => r.resolved_findings), borderColor: '#4ade80', backgroundColor: 'rgba(74,222,128,0.4)', fill: true, tension: 0.25 }
    ] },
    options: { ...opts, scales: { ...opts.scales, y: { ...opts.scales.y, stacked: true }, x: { ...opts.scales.x, stacked: true } } }
  });

  new Chart(document.getElementById('traffic'), {
    type: 'bar',
    data: { labels: ['green (low)', 'yellow (mid)', 'red (high)'],
      datasets: [{ label: 'runs', data: traffic,
        backgroundColor: ['#4ade80', '#fbbf24', '#f87171'] }] },
    options: { ...opts, plugins: { legend: { display: false } } }
  });
</script>
{{end}}
</main>
</body>
</html>
`
