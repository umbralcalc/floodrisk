# Flood Risk & Climate Adaptation Simulation

## Applying the Stochadex to Catchment-Level Flood Policy Optimisation

This project builds a stochastic simulation of catchment-scale flood dynamics under climate change, learned from freely available UK hydrological data, with a decision science layer to evaluate natural flood management (NFM) intervention portfolios. It uses the [stochadex](https://github.com/umbralcalc/stochadex) simulation SDK.

The core question: **given a catchment and projected climate trajectories, what combination of NFM interventions minimises expected flood damage, and where should they be placed?**

---

## Study Area

The **Upper Calder Valley** in West Yorkshire was chosen for its extensive EA monitoring network, history of severe flooding (notably Boxing Day 2015), active NFM schemes, and good data coverage. The catchment is decomposed into 4 active sub-catchments:

| Sub-catchment | Area (km²) | Gauge |
|---------------|-----------|-------|
| Ryburn | 25 | Ryburn at Ripponden |
| Colne | 195 | Colne at Colne Bridge |
| Holme | 50 | Holme at Holmfirth |
| Upper Calder | 70 | Residual headwater area |

The downstream integration point is the Elland gauging station on the River Calder.

---

## Data

All data is freely available from Environment Agency open APIs under the Open Government Licence:

- **7 flow gauging stations** — River Calder at Elland and Dewsbury, plus tributaries Ryburn, Colne, Holme, and Spen Beck
- **24 rainfall stations** within 15 km of the catchment centre
- **109 flood alert/warning areas** covering the catchment
- **16 years of daily data** (2010–2025)

Exploratory analysis confirms the Boxing Day 2015 flood as the largest event in the record (189.66 m³/s at Elland, ~17-year empirical return period on annual maxima).

Data is downloaded and stored locally in `dat/` (gitignored). Regenerate with:

```bash
go run ./cmd/ingest/
```

The `-data` flag overrides the output directory; `-from`/`-to` control the date range.

---

## Model

### Rainfall-Runoff

The core hydrological model is a **PDM-style lumped rainfall-runoff** simulation (`RainfallRunoffIteration`) with:

- Nonlinear runoff generation via a PDM exponent controlling partial-area saturation
- Parallel fast (surface) and slow (baseflow) routing stores
- State vector: `[soil_moisture_mm, total_flow_m3s, fast_flow_m3s, slow_flow_m3s]`
- 7 calibration parameters: field capacity, drainage rate, ET rate, runoff shape, fast/slow recession rates, catchment area

### Channel Routing

Sub-catchment flows are aggregated via a **linear reservoir routing model** (`ChannelRoutingIteration`):

```
routed_i(t) = K_i * upstream_i(t) + (1 - K_i) * routed_i(t-1)
```

with per-sub-catchment routing coefficients K_i. This is where leaky dam and floodplain reconnection interventions act — by reducing K, they attenuate and delay peak flows.

### Stochastic Rainfall Generator

A **two-state Markov chain** (`StochasticRainfallIteration`) generates synthetic daily rainfall:

- Wet/dry day transitions fitted from 16 years of observations
- Gamma-distributed wet-day amounts
- Climate perturbation via a multiplicative `rainfall_multiplier` parameter

Fitted parameters from the Upper Calder data:

| Parameter | Value |
|-----------|-------|
| Gamma shape | 0.61 |
| Gamma scale | 7.94 mm |
| P(wet\|dry) | 0.40 |
| P(wet\|wet) | 0.83 |

---

## Calibration & Validation

### Single-Catchment Calibration

Random-search calibration (5000 trials) against observed daily flow at Elland:

| Metric | Value |
|--------|-------|
| Nash-Sutcliffe Efficiency | 0.34 |
| Volume error | ~0.02 |
| Peak flow bias | -0.68 |

Best-fit parameters: FC=298 mm, drainage rate=0.0265, ET=0.86 mm/day, runoff shape=2.21, fast recession=0.35, slow recession=0.44, area=282 km².

### Temporal Holdout

Trained on 2010–2022 (4748 days), tested on 2023–2025 (1095 days):

| Period | NSE |
|--------|-----|
| Training | 0.34 |
| Holdout | 0.31 |

The small drop confirms the model generalises without significant overfitting.

### Flood Event Reproduction

150 flood events detected above the P95 threshold (≈30.7 m³/s). The model correctly identifies the Boxing Day 2015 flood as the largest event (observed 189.66 m³/s, simulated 59.22 m³/s). Mean absolute peak bias across all events is 0.60 — the lumped PDM consistently underestimates extreme peaks, a known limitation of this model class.

### Multi-Catchment

Multi-sub-catchment calibration with shared PDM parameters achieves NSE ≈ 0.23 (5000 trials), below the single-catchment baseline. This is expected given the uneven rainfall station distribution across sub-catchments and the shared parameter constraint.

### Simulation-Based Inference

Posterior estimation uses the stochadex `analysis.NewPosteriorEstimationPartitions` builder — windowed embedded simulations with Normal likelihood comparison, online posterior mean and covariance tracking, and past-discounting. Available in both single-catchment and multi-catchment configurations.

---

## NFM Interventions

Four intervention types are modelled as **stochastic parameter modifiers**, with effectiveness priors drawn from the EA Working with Natural Processes (WWNP) evidence directory. Each ensemble member samples its own effectiveness values, propagating uncertainty.

A **linear-with-cap** model is used for routing interventions to avoid unrealistic compound effects:

```
total_reduction = min(Scale / FullScale * sampled_max, sampled_max)
routing_factor = 1.0 - total_reduction
```

| Intervention | Mechanism | Prior Range |
|-------------|-----------|-------------|
| Leaky dams | Routing attenuation (reduces K) | 5–15% total reduction at full deployment |
| Woodland planting | +Field capacity, +ET rate | +5–30 mm FC, +0.1–0.5 mm/day ET per 10 ha |
| Floodplain reconnection | Routing attenuation (off-channel storage) | 5–15% reduction per site, up to 3 sites |
| Peat restoration | +Field capacity (headwater storage) | +10–40 mm per 10 ha |

### Candidate Portfolios

| Portfolio | Interventions | Cost |
|-----------|--------------|------|
| No intervention | Baseline | £0 |
| Leaky dams only | 40 clusters across Ryburn, Upper Calder, Colne | £500k |
| Woodland focus | 120 ha across Ryburn, Upper Calder, Colne | £1M |
| Mixed | Leaky dams + woodland + floodplain reconnection + peat | £2M |

---

## Climate Scenarios

UKCP18-informed rainfall intensity multipliers:

| Scenario | Rainfall Multiplier | Interpretation |
|----------|-------------------|----------------|
| Baseline | 1.00 | Current climate |
| RCP4.5 2040 | 1.10 | +10% wet-day intensity |
| RCP4.5 2070 | 1.20 | +20% wet-day intensity |
| RCP8.5 2040 | 1.15 | +15% wet-day intensity |
| RCP8.5 2070 | 1.35 | +35% wet-day intensity |

---

## Policy Evaluation Results

50-member ensembles, 10-year (3650-day) simulations per portfolio per scenario.

### Peak Flow Distributions (m³/s)

| Portfolio | Scenario | Mean Peak | Std Peak | P95 Peak | Max Peak |
|-----------|----------|----------|---------|---------|---------|
| no_intervention | baseline | 41.58 | 6.06 | 52.96 | 55.92 |
| no_intervention | RCP4.5_2040 | 47.43 | 6.88 | 60.08 | 64.09 |
| no_intervention | RCP4.5_2070 | 53.31 | 7.62 | 68.56 | 72.44 |
| no_intervention | RCP8.5_2040 | 50.51 | 7.27 | 64.15 | 68.24 |
| no_intervention | RCP8.5_2070 | 62.88 | 8.99 | 80.84 | 85.25 |
| leaky_dams_only | baseline | 39.55 | 5.73 | 50.12 | 53.06 |
| leaky_dams_only | RCP8.5_2070 | 59.81 | 8.52 | 76.12 | 80.89 |
| woodland_focus | baseline | 36.09 | 5.42 | 45.29 | 51.04 |
| woodland_focus | RCP8.5_2070 | 56.28 | 8.20 | 72.17 | 79.51 |
| mixed_portfolio | baseline | 36.79 | 5.49 | 47.09 | 49.21 |
| mixed_portfolio | RCP8.5_2070 | 56.48 | 8.23 | 72.57 | 76.43 |

### Peak Flow Reduction vs No Intervention

| Portfolio | Baseline | RCP4.5 2040 | RCP4.5 2070 | RCP8.5 2040 | RCP8.5 2070 |
|-----------|---------|------------|------------|------------|------------|
| Leaky dams only (£500k) | 4.9% | 4.9% | 4.9% | 4.9% | 4.9% |
| Woodland focus (£1M) | 13.2% | 12.3% | 11.5% | 11.9% | 10.5% |
| Mixed portfolio (£2M) | 11.5% | 11.1% | 10.7% | 10.9% | 10.2% |

### Key Findings

1. **Woodland planting is the most cost-effective intervention.** At £1M, the woodland focus portfolio achieves 13.2% peak reduction under baseline climate — more than double the 4.9% from £500k of leaky dams, and outperforming the £2M mixed portfolio (11.5%).

2. **NFM effectiveness declines under extreme climate.** Woodland reduction falls from 13.2% at baseline to 10.5% under RCP8.5 2070. The interventions reduce absolute flow levels, but the percentage reduction shrinks as the climate-driven peak grows. Leaky dam effectiveness is constant (4.9%) because routing attenuation is a fixed proportional effect.

3. **NFM alone cannot offset climate-driven increases.** Even with the best portfolio, mean peak flow under no intervention rises from about 42 m³/s at baseline to about 63 m³/s under RCP8.5 2070 (roughly +51% vs that baseline). NFM buys time and reduces severity, but does not eliminate climate risk.

4. **More investment is not always better.** The £2M mixed portfolio underperforms the £1M woodland focus. This reflects the interaction between intervention types — mixing interventions dilutes the allocation to the most effective one (woodland) without enough compensating benefit from the others.

5. **Nonlinear climate amplification.** A 35% increase in rainfall intensity (RCP8.5 2070) produces about a 51% increase in mean peak flow with no intervention (41.6→62.9 m³/s), demonstrating how catchment hydrology amplifies rainfall changes through soil saturation and nonlinear runoff generation.

---

## Visualisations

Interactive plots are available in the GoNB notebook at [`nbs/policy_evaluation.ipynb`](nbs/policy_evaluation.ipynb). The notebook reproduces the full pipeline — calibration, rainfall generator fitting, policy evaluation, and plotting — using the [GoNB](https://github.com/janpfeifer/gonb) Jupyter kernel with [go-echarts](https://github.com/go-echarts/go-echarts) scatter plots via the stochadex `analysis.NewScatterPlotFromDataFrame` helper.

Plots include:
- Mean and P95 peak flow by portfolio and climate scenario
- Percentage peak flow reduction vs no intervention
- Climate sensitivity: rainfall multiplier vs mean peak flow, grouped by portfolio

---

## Running the Code

### Prerequisites

- Go 1.22+
- Internet access for initial data download

### Commands

```bash
go build ./...                        # compile
go test -count=1 ./...                # run all tests

go run ./cmd/ingest/                  # download EA data → dat/
go run ./cmd/analyse/                 # exploratory analysis on dat/
go run ./cmd/calibrate/               # single-catchment calibration
go run ./cmd/calibrate/ -multi        # multi-sub-catchment calibration
go run ./cmd/sbi/                     # single-catchment SBI
go run ./cmd/sbi/ -multi              # multi-sub-catchment SBI
go run ./cmd/evaluate/ -cal-trials 5000 # NFM policy evaluation (README tables use 5000 calibration trials)
```

### Project Structure

```
cmd/ingest/       Download EA Hydrology, Rainfall, and Flood area data
cmd/analyse/      Exploratory analysis on downloaded data
cmd/calibrate/    Random-search model calibration
cmd/sbi/          Simulation-based inference (posterior estimation)
cmd/evaluate/     NFM policy evaluation across climate scenarios
pkg/hydrology/    EA API client, catchment config, data ingestion, alignment, metrics
pkg/catchment/    Rainfall-runoff model, calibration, SBI, interventions, policy evaluation
cfg/              Stochadex YAML simulation configs
dat/              Downloaded CSV data (gitignored, regenerable via cmd/ingest)
nbs/              GoNB Jupyter notebooks for interactive analysis and visualisation
```

---

## Limitations & Future Work

**Current limitations:**

- The lumped PDM model consistently underestimates extreme peaks (mean absolute peak bias 0.60). A distributed or event-based model would improve peak reproduction.
- Single daily timestep misses sub-daily flood dynamics. Hourly resolution would better capture flashy upland responses.
- NFM effectiveness priors are drawn from literature ranges, not catchment-specific monitoring. Local calibration data would reduce uncertainty.
- The stochastic rainfall generator uses a simple wet/dry Markov chain. Multi-site spatial correlation and seasonal variation are not captured.

**Planned extensions:**

- **Multi-catchment library** — repeat for 5–10 UK catchments with different characteristics to build transferable insights
- **Grey + green integration** — add engineered defences alongside NFM to evaluate hybrid portfolios
- **Dynamic adaptation pathways** — model sequential decision-making ("plant woodland now, add leaky dams in year 5 if trends are adverse")
- **Insurance integration** — connect flood damage outputs to insurance loss models
- **Real-time operational mode** — connect to live EA flood monitoring for real-time decisions during events

---

## Data Sources

| Source | Data | Access |
|--------|------|--------|
| EA Hydrology API | River flow, level, groundwater, rainfall | Open Government Licence |
| EA Flood Monitoring API | Real-time flood warnings, levels, flows | Open Government Licence |
| NRFA | UK river flow records, peak flow dataset | Free download |
| UKCP18 | Climate projections (probabilistic, 12km, 2.2km) | CEDA Archive |
| EA WWNP Evidence Directory | 700+ studies on NFM effectiveness | Free PDF |

---

## References

- EA "Working with Natural Processes" evidence directory (updated February 2025)
- Leaky dam quantification study — transfer function noise modelling for 50 upland storm events showing ~10% peak reduction for ≤1yr return period (ScienceDirect, 2023)
- Pickering "Slowing the Flow" — combined riparian woodland, leaky structures, and bund reduced peak flows by 15–20%
- Wildlife Trusts NFM research — 10 schemes averaged 4:1 cost-benefit over 10 years, 10:1 over 30 years
- River Otter beaver trial — beaver dams attenuated flood flows by ~30% on average
- UKCP18 Local (2.2km) projections — convection-permitting model capturing extreme rainfall events
