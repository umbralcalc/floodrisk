// generate emits the floodrisk widget shell (widget.html, test.html,
// build.sh) into app/flood/. Re-run whenever the dashboard.Config in
// pkg/flooddash changes shape (controls, partitions, visualisation).
//
//	cd app && go run ./cmd/flood/generate
//
// After codegen the emitted HTML is post-processed to:
//   - recolour the slider accent + readout text in the explainer-series'
//     magenta so the controls read as "what the reader does"
//   - inject DOM captions around the canvas describing each panel
//     (dexetera's text renderer hardcodes a white fill which is
//     invisible on our light background, but we also want a single
//     above-canvas caption for a screen-reader-friendly title)
//   - replace the dexetera-emitted portfolio and scenario sliders with
//     two rows of radio buttons, so the categorical choices get
//     categorical controls rather than the numeric sliders they ship
//     with
//   - patch the dexetera-emitted IIFE so the worker is terminated and
//     a status message is posted once the SimMembers horizon is
//     reached (otherwise the inline driver ticks forever even after
//     iterations freeze)
//   - re-publish slider values on the 'inline driver ready' status
//     message so the very-first action message lands after the worker
//     has its driver loaded
//   - load the worker via a same-origin Blob URL so cross-origin
//     CDN hosting works
//   - wire the radio buttons to the hidden sliders + the Reset button
//     so a portfolio/scenario change restarts the ensemble
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/umbralcalc/dexetera/pkg/dashboard"
	"github.com/umbralcalc/floodrisk/app/pkg/flooddash"
)

// actionColor is the magenta from the Acting on Simulated Systems
// collection — used to signal "this is what the reader controls".
// Replaces dexetera's default blue (#3c78d8) on the slider track and
// the slider readout text. Kept in sync with flooddash.actionColorHex.
const actionColor = "#b0447a"

// portfolioChoices lists the radio buttons that replace the portfolio
// slider. Indices match flooddash.Portfolio* constants.
var portfolioChoices = []struct {
	Value int
	Label string
}{
	{flooddash.PortfolioNoIntervention, "No intervention (£0)"},
	{flooddash.PortfolioLeakyDams, "Leaky dams (£500k)"},
	{flooddash.PortfolioWoodland, "Woodland focus (£1M)"},
	{flooddash.PortfolioMixed, "Mixed (£2M)"},
}

// scenarioChoices lists the radio buttons that replace the scenario
// slider. Indices match flooddash.Scenario* constants.
var scenarioChoices = []struct {
	Value int
	Label string
}{
	{flooddash.ScenarioBaseline, "Baseline climate"},
	{flooddash.ScenarioRCP45_2040, "RCP4.5 — 2040 (+10%)"},
	{flooddash.ScenarioRCP45_2070, "RCP4.5 — 2070 (+20%)"},
	{flooddash.ScenarioRCP85_2040, "RCP8.5 — 2040 (+15%)"},
	{flooddash.ScenarioRCP85_2070, "RCP8.5 — 2070 (+35%)"},
}

func main() {
	runtimeURL := flag.String("runtime-url", "",
		"absolute URL the blog will serve dexetera's runtime/ folder from "+
			"(e.g. https://example.com/assets/dexetera/runtime/). "+
			"Leave empty for local preview via test.html.")
	wasmURL := flag.String("wasm-url", "",
		"absolute URL the blog will serve main.wasm from. "+
			"Leave empty for local preview.")
	flag.Parse()

	cfg := flooddash.NewConfig()
	dashboard.MustGenerateWidget(cfg, dashboard.WidgetOptions{
		RuntimeBaseURL: *runtimeURL,
		WasmURL:        *wasmURL,
	})

	for _, name := range []string{"widget.html", "test.html"} {
		path := filepath.Join(cfg.Name, name)
		if err := postProcess(path); err != nil {
			fmt.Fprintf(os.Stderr, "post-process %s: %v\n", path, err)
			os.Exit(1)
		}
	}
}

func postProcess(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	out := string(data)
	widgetID := extractWidgetID(out)

	for _, step := range []func(string, string) (string, error){
		recolorControls,
		injectScopedStyles,
		replaceSliderWithRadios("portfolio", "flood-portfolio", "NFM portfolio", portfolioChoiceLabels()),
		replaceSliderWithRadios("scenario", "flood-scenario", "Climate scenario", scenarioChoiceLabels()),
		injectTerminationHalt,
		injectActionResend,
		injectCrossOriginWorkerShim,
		injectControlScript,
	} {
		out, err = step(out, widgetID)
		if err != nil {
			return err
		}
	}
	return os.WriteFile(path, []byte(out), 0644)
}

func portfolioChoiceLabels() []choiceLabel {
	out := make([]choiceLabel, len(portfolioChoices))
	for i, c := range portfolioChoices {
		out[i] = choiceLabel{Value: c.Value, Label: c.Label}
	}
	return out
}

func scenarioChoiceLabels() []choiceLabel {
	out := make([]choiceLabel, len(scenarioChoices))
	for i, c := range scenarioChoices {
		out[i] = choiceLabel{Value: c.Value, Label: c.Label}
	}
	return out
}

type choiceLabel struct {
	Value int
	Label string
}

// recolorControls swaps dexetera's default blue on the slider accent
// and readout for the action-colour magenta. Anchored on enough
// surrounding CSS text to avoid touching unrelated occurrences of the
// colour.
func recolorControls(html, _ string) (string, error) {
	pairs := [][2]string{
		{"accent-color: #3c78d8", "accent-color: " + actionColor},
		{".slider-readout { grid-area: readout; text-align: right; color: #3c78d8;",
			".slider-readout { grid-area: readout; text-align: right; color: " + actionColor + ";"},
	}
	return applyPairs(html, pairs)
}

// injectScopedStyles appends CSS rules for the two radio-button rows
// (portfolio and scenario selectors). All rules are prefixed with
// #<widgetID> so they don't leak out of the widget shell.
func injectScopedStyles(html, widgetID string) (string, error) {
	const marker = `</style>`
	extra := strings.ReplaceAll(scopedStylesTemplate, "{{.WidgetID}}", widgetID)
	if !strings.Contains(html, marker) {
		return "", fmt.Errorf("</style> marker not found")
	}
	return strings.Replace(html, marker, extra+marker, 1), nil
}

const scopedStylesTemplate = `#{{.WidgetID}} .flood-selector { display: flex; flex-direction: column; gap: 0.4em; font-size: 1rem; margin-bottom: 0.6em; }` +
	`#{{.WidgetID}} .flood-selector-label { color: #2c3e50; font-weight: 600; }` +
	`#{{.WidgetID}} .flood-options { display: flex; flex-direction: column; gap: 0.3em; }` +
	`#{{.WidgetID}} .flood-options label { display: flex; align-items: center; gap: 0.4em; color: #2c3e50; cursor: pointer; }` +
	`#{{.WidgetID}} .flood-options input[type="radio"] { accent-color: ` + actionColor + `; }`

// replaceSliderWithRadios returns a post-process step that rewrites
// the dexetera-emitted slider for the given sliderName into a
// radio-button group with the given heading. The slider input itself
// is kept (display:none) so the existing slider→worker publish
// mechanism in runtime/worker.js can still pick it up — our injected
// JS just keeps the hidden slider's value in sync with whichever
// radio button is selected.
func replaceSliderWithRadios(sliderName, radioName, heading string, choices []choiceLabel) func(string, string) (string, error) {
	return func(html, _ string) (string, error) {
		startTag := `<label class="slider">`
		endTag := `</label>`
		dataAttr := `data-slider="` + sliderName + `"`

		idx := strings.Index(html, dataAttr)
		if idx == -1 {
			return "", fmt.Errorf("%s slider not found", sliderName)
		}
		start := strings.LastIndex(html[:idx], startTag)
		if start == -1 {
			return "", fmt.Errorf("%s slider <label> not found", sliderName)
		}
		end := strings.Index(html[idx:], endTag)
		if end == -1 {
			return "", fmt.Errorf("%s slider </label> not found", sliderName)
		}
		end += idx + len(endTag)

		var b strings.Builder
		b.WriteString(`<div class="flood-selector">`)
		fmt.Fprintf(&b, `<span class="flood-selector-label">%s</span>`, heading)
		b.WriteString(`<div class="flood-options">`)
		for _, c := range choices {
			checked := ""
			if c.Value == 0 {
				checked = " checked"
			}
			fmt.Fprintf(&b,
				`<label><input type="radio" name="%s" value="%d"%s>%s</label>`,
				radioName, c.Value, checked, c.Label,
			)
		}
		b.WriteString(`</div>`)
		// Keep the original slider input hidden inside the selector so
		// the publish mechanism still finds it. The radio handlers in
		// the injected script write to this input's value and dispatch
		// an 'input' event so the existing change listener fires.
		fmt.Fprintf(&b,
			`<input type="range" data-slider="%s" min="0" max="%d" step="1" value="0" style="display:none">`,
			sliderName, len(choices)-1,
		)
		fmt.Fprintf(&b, `<span data-slider-readout="%s" style="display:none"></span>`, sliderName)
		b.WriteString(`</div>`)

		return html[:start] + b.String() + html[end:], nil
	}
}

// injectTerminationHalt patches the dexetera-emitted IIFE so that
// when a partition state arrives with cumulativeTimesteps >=
// SimMembers + 1 (one extra step to flush the final stats through
// the readouts), the worker is terminated and a status message is
// posted. Without this the inline driver ticks forever — even with
// frozen iterations, the renderer keeps re-drawing identical state
// every 30 ms.
//
// Placement matters: the check must run *after* the readout-update
// loop, otherwise the final-state values never reach the on-page
// readouts (the early-return skips the loop, leaving them showing
// the t=N-1 snapshot). We attach to the closing brace of the
// partitionState branch — by then renderer.update/render and all
// readout writes have completed for this message.
func injectTerminationHalt(html, _ string) (string, error) {
	const oldTail = `if (el) el.textContent = applyReadout(r.template, r.decimals, msg.data);
                }
            } else if (msg.type === 'status') {`
	newTail := fmt.Sprintf(`if (el) el.textContent = applyReadout(r.template, r.decimals, msg.data);
                }
                if (worker && msg.data.timesteps >= %d) { worker.terminate(); worker = null; setStatus('Ensemble complete (%d members). Use Reset to rerun.'); }
            } else if (msg.type === 'status') {`,
		flooddash.SimMembers, flooddash.SimMembers)
	if !strings.Contains(html, oldTail) {
		return "", fmt.Errorf("partitionState block anchor not found for termination halt")
	}
	return strings.Replace(html, oldTail, newTail, 1), nil
}

// injectActionResend patches the dexetera-emitted IIFE so that the
// page re-publishes the current slider values once the driver
// reports that it's ready.
//
// Why this is needed: startWorker posts {action:'start'} to the
// worker and then *immediately* calls publishActions(), which posts
// {action:'setActions', ...}. But the worker handles 'start' by
// awaiting loadWasm() and then importScripts(driver) — both async —
// so the setActions message lands in the worker before any
// subscriber is listening for it. The inline driver only registers
// its onPageMessage handler inside its start() function, which only
// runs after the driver script has loaded.
//
// The dropped message means the *first* step (and possibly several
// more) runs with the wasm's initial action_state_values — which is
// fine on the very first page load (defaults match) but produces the
// wrong portfolio/scenario on Reset after the radio buttons changed
// the slider.
//
// Republishing on the 'inline driver ready' status message ensures
// the new worker actually receives the current slider state before
// its first action-consuming tick.
func injectActionResend(html, _ string) (string, error) {
	const oldStatus = `} else if (msg.type === 'status') {
                setStatus(msg.data);`
	const newStatus = `} else if (msg.type === 'status') {
                setStatus(msg.data);
                if (msg.data === 'inline driver ready') publishActions();`
	if !strings.Contains(html, oldStatus) {
		return "", fmt.Errorf("status handler anchor not found for action resend")
	}
	return strings.Replace(html, oldStatus, newStatus, 1), nil
}

// injectCrossOriginWorkerShim wraps the dexetera-emitted worker
// creation so the worker.js script can be loaded from a different
// origin (e.g. the blog's R2 CDN) while the page itself is served
// from GitHub Pages. Mirrors the pattern used by the AMR and rugby
// widgets; lift this into dexetera proper once it stabilises.
func injectCrossOriginWorkerShim(html, _ string) (string, error) {
	const oldNewWorker = `worker = new Worker(RUNTIME_BASE + 'worker.js');`
	const newNewWorker = `ensureWorkerUrl().then(function (workerUrl) {
        worker = new Worker(workerUrl);`
	if !strings.Contains(html, oldNewWorker) {
		return "", fmt.Errorf("worker creation anchor not found for cross-origin shim")
	}
	html = strings.Replace(html, oldNewWorker, newNewWorker, 1)

	const oldEnd = `        publishActions();
    }

    ensureRenderer().then(function () {`
	const newEnd = `        publishActions();
        }).catch(function (err) {
            console.error(err);
            setStatus('Failed to load dexetera worker: ' + err.message);
        });
    }

    ensureRenderer().then(function () {`
	if !strings.Contains(html, oldEnd) {
		return "", fmt.Errorf("startWorker tail anchor not found for cross-origin shim")
	}
	html = strings.Replace(html, oldEnd, newEnd, 1)

	const startWorkerSig = `function startWorker(renderer) {`
	const ensureWorkerUrlFn = `function ensureWorkerUrl() {
        if (self.__dexeteraWorkerUrl) return Promise.resolve(self.__dexeteraWorkerUrl);
        if (self.__dexeteraWorkerLoading) return self.__dexeteraWorkerLoading;
        // Resolve RUNTIME_BASE against the page URL so the shim's base
        // is absolute. Once the worker starts from a blob: URL it has
        // no document context, so any relative base (e.g. the local
        // preview's '../runtime/') would fail with "Invalid base URL"
        // inside the importScripts wrapper.
        var BASE_ABS = new URL(RUNTIME_BASE, document.baseURI).href;
        self.__dexeteraWorkerLoading = fetch(BASE_ABS + 'worker.js')
            .then(function (r) {
                if (!r.ok) throw new Error('failed to fetch worker.js: ' + r.status);
                return r.text();
            })
            .then(function (src) {
                var shim = '(function(){var BASE=' + JSON.stringify(BASE_ABS) +
                    ';var orig=self.importScripts;self.importScripts=function(){' +
                    'var args=Array.prototype.map.call(arguments,function(u){' +
                    'return new URL(u,BASE).href;});return orig.apply(self,args);};})();\n';
                var blob = new Blob([shim, src], { type: 'application/javascript' });
                self.__dexeteraWorkerUrl = URL.createObjectURL(blob);
                return self.__dexeteraWorkerUrl;
            });
        return self.__dexeteraWorkerLoading;
    }

    `
	if !strings.Contains(html, startWorkerSig) {
		return "", fmt.Errorf("startWorker signature not found for cross-origin shim")
	}
	html = strings.Replace(html, startWorkerSig, ensureWorkerUrlFn+startWorkerSig, 1)
	return html, nil
}

// injectControlScript appends a small IIFE that wires the radio
// buttons (both portfolio and scenario groups) to their hidden
// sliders and clicks the Reset button on every change. The "click
// Reset to restart" pattern is the same one the AMR widget uses for
// its categorical policy choice.
func injectControlScript(html, widgetID string) (string, error) {
	script := strings.ReplaceAll(controlScriptTemplate, "{{.WidgetID}}", widgetID)
	return html + script, nil
}

const controlScriptTemplate = `
<script>
(function () {
    var widget = document.getElementById('{{.WidgetID}}');
    if (!widget) return;

    function wireGroup(radioName, sliderName) {
        var radios = widget.querySelectorAll('input[name="' + radioName + '"]');
        var slider = widget.querySelector('[data-slider="' + sliderName + '"]');
        var resetBtn = widget.querySelector('[data-reset]');

        function applyValue(value) {
            if (slider) {
                slider.value = String(value);
                slider.dispatchEvent(new Event('input', { bubbles: true }));
            }
        }
        for (var i = 0; i < radios.length; i++) {
            radios[i].addEventListener('change', function (e) {
                applyValue(parseInt(e.target.value, 10));
                if (resetBtn) resetBtn.click();
            });
        }
        var initial = 0;
        for (var j = 0; j < radios.length; j++) {
            if (radios[j].checked) { initial = parseInt(radios[j].value, 10); break; }
        }
        // Initial state — sync slider but do NOT click Reset (the
        // dexetera IIFE handles the very first sim start; clicking
        // Reset on init would race the renderer load).
        applyValue(initial);
    }

    wireGroup('flood-portfolio', 'portfolio');
    wireGroup('flood-scenario', 'scenario');
})();
</script>
`

func applyPairs(html string, pairs [][2]string) (string, error) {
	for _, p := range pairs {
		if !strings.Contains(html, p[0]) {
			return "", fmt.Errorf("expected fragment not found: %q", p[0])
		}
		html = strings.Replace(html, p[0], p[1], 1)
	}
	return html, nil
}

// extractWidgetID picks the widget root's id out of the generated
// HTML so the styles and script we inject can scope to the same
// element as the rest of the dexetera CSS.
func extractWidgetID(html string) string {
	const marker = `id="`
	i := strings.Index(html, marker)
	if i < 0 {
		return "dexetera"
	}
	i += len(marker)
	end := strings.Index(html[i:], `"`)
	if end < 0 {
		return "dexetera"
	}
	return html[i : i+end]
}
