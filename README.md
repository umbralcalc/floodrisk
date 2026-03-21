# Flood Risk & Climate Adaptation Simulation: Project Plan

## Applying the Stochadex to Catchment-Level Flood Policy Optimisation

---

## Overview

Build a stochastic simulation of catchment-scale flood dynamics under climate change, learned from freely available UK hydrological, meteorological and land-use data, with a decision science layer to evaluate and optimise natural flood management (NFM) intervention portfolios.

The core question: **given the current state of a catchment and projected climate trajectories, what combination of interventions minimises expected flood damage over 10–50 years, and where should they be placed?**

---

## Why This Problem

- Flooding is the UK's most expensive natural hazard, costing approximately £2.2 billion annually, projected to rise 19–49% by the 2050s according to the UK Climate Change Risk Assessment.
- One in six houses across the UK is currently at risk of flooding.
- Under a high emissions scenario, UKCP18 projections suggest UK rainfall events exceeding 20mm/hr could be four times as frequent by 2080 compared to the 1980s.
- Natural flood management (NFM) schemes show promising cost-benefit ratios — Wildlife Trust research found an average 4:1 over 10 years, rising to 10:1 over 30 years — but councils and water companies lack good tools for deciding *which* interventions to deploy, *where* in a catchment, and *in what combination*.
- Evidence on NFM effectiveness is highly variable: leaky dams reduced flood peaks by ~10% on average for events up to 1-year return period in one upland study, but effectiveness depends heavily on event magnitude, catchment characteristics, and spatial placement. Existing models rarely propagate this uncertainty properly.

---

## The Gap This Fills

Existing flood modelling falls into camps that don't do what the stochadex enables:

| Approach | Examples | Limitation |
|----------|----------|------------|
| Deterministic hydrodynamic models | HEC-RAS, TUFLOW, MIKE FLOOD, LISFLOOD-FP | Excellent for single-event flood mapping, but don't natively handle stochastic rainfall, uncertain NFM effectiveness, or policy optimisation |
| Rainfall-runoff models | HEC-HMS, SWMM, SWAT | Good at catchment hydrology, but typically event-based or deterministic continuous; don't integrate decision science |
| Statistical flood frequency analysis | FEH methods, extreme value distributions | Estimate return periods but can't evaluate intervention portfolios or simulate NFM effects |
| Machine learning approaches | Random forests, ANNs for flood susceptibility mapping | Predict flood risk from static features but don't simulate dynamic interventions or future climate |

**The stochadex differentiator:** a generalised stochastic simulation that learns catchment dynamics from observed hydrological data, propagates rainfall uncertainty from climate projections through the system, models NFM interventions as stochastic modifiers of flow attenuation, and uses the decision science layer to optimise intervention portfolios — the same "simulate, learn, decide" pattern proven across rugby, fishing, COVID, helminth, and LOB projects.

---

## Phase 1: Data Ingestion

### 1.1 Hydrological monitoring data

**Source: Environment Agency Hydrology API**

- Open data under the Open Government Licence, no registration required
- Nearly 8,000 monitoring stations across England
- Sub-daily (15-minute) resolution time series for river flows, river levels, groundwater levels, and rainfall
- Quality-checked and qualified archive data, many records back to the 1960s+
- CSV and JSON download via REST API

**API base:** `https://environment.data.gov.uk/hydrology/`

**Key endpoints:**
```
# List all flow stations
/hydrology/id/stations?observedProperty=waterFlow

# Get 15-min flow readings for a station
/hydrology/id/measures/{measure-id}/readings?mineq-date=2020-01-01&max-date=2024-01-01

# Daily mean flows (qualified, quality-checked)
/hydrology/id/measures/{measure-id}-flow-m-86400-m3s-qualified/readings
```

**Source: National River Flow Archive (NRFA)**

- UK's official record of river flow data, over 1,600 gauging stations
- Peak flow dataset for flood frequency estimation (downloadable for WINFAP software)
- Long-term records enabling trend analysis for climate change impact assessment

### 1.2 Real-time flood monitoring

**Source: EA Flood Monitoring API**

- Real-time flood warnings, alerts, and 3-day risk forecasts
- Water level and flow measurements, typically every 15 minutes
- Historical flood event data
- Open data, no registration

**API base:** `https://environment.data.gov.uk/flood-monitoring/`

**Key endpoints:**
```
# Current flood warnings
/flood-monitoring/id/floods

# All latest readings
/flood-monitoring/data/readings?latest

# Historical archive (daily CSV dumps)
/flood-monitoring/archive/readings-{date}.csv
```

### 1.3 Rainfall data

**Source: EA Rainfall API**

- Part of the 10-API EA catalogue alongside hydrology, flood monitoring, water quality, etc.
- Station-based rainfall time series, sub-daily resolution

**Source: Met Office HadUK-Grid (via UKCP User Interface)**

- Gridded observations at 1km resolution
- Daily rainfall, temperature, and other variables
- Available through the UKCP User Interface (registration required) and CEDA Archive

### 1.4 Climate projections

**Source: UKCP18 (UK Climate Projections 2018)**

- Probabilistic projections across five emission scenarios (RCP2.6 to RCP8.5)
- Global (60km), Regional (12km), and Local (2.2km convection-permitting) resolutions
- Precipitation, temperature, and other variables
- Available via CEDA Archive and UKCP User Interface
- Bias-corrected versions available from UEA CRU under Open Database Licence

**Key dataset for this project:** UKCP18 convection-permitting model (CPM) widespread rainfall events for three periods (1980–2000, 2020–2040, 2060–2080), covering ~72,000 event summaries under RCP8.5 for 12 ensemble members. Available under Open Government Licence.

### 1.5 Flood risk and catchment data

**Source: EA Risk of Flooding from Rivers and Sea**

- Open data (since December 2014) showing flood likelihood across England
- Considers location, height, and condition of flood defences
- Four risk categories, available at postcode level

**Source: EA Catchment Data API**

- Catchment boundaries and characteristics
- Land use, soil type, and topographic data by catchment

### 1.6 Natural flood management evidence

**Source: EA Working with Natural Processes (WWNP) Evidence Directory**

- Over 700 studies on NFM effectiveness compiled by the Environment Agency
- Quantitative evidence on leaky dams, woodland planting, peat restoration, floodplain reconnection, beaver reintroduction
- Site-specific monitoring data from schemes like Pickering ("Slowing the Flow"), Pontbren, and River Otter beaver trial

### 1.7 Initial data scope

Start with a single, well-instrumented catchment to prove the concept:

- **Primary candidate:** Upper Calder Valley or Aire catchment (Yorkshire) — extensive EA monitoring, history of flooding (Boxing Day 2015), active NFM schemes, good data coverage
- **Alternative:** Severn upper catchment — long flow records, mixed land use, LISFLOOD-FP already validated there
- **Time window:** 2010–2025 for model fitting, UKCP18 projections to 2080 for policy evaluation
- **Resolution:** Sub-daily (hourly aggregation of 15-min data) to capture flood peak dynamics

---

## Phase 2: Model Structure

### 2.1 State variables

The stochadex simulation tracks the catchment as a coupled stochastic system:

1. **Rainfall process** — stochastic rainfall event generation, learned from observed distributions and perturbed by UKCP18 climate scenarios. This is the exogenous driver.
2. **Soil moisture state** — antecedent wetness conditions determining runoff generation, with stochastic dynamics driven by rainfall, evapotranspiration, and drainage
3. **Runoff generation** — stochastic transformation of rainfall to runoff, modulated by land use, soil type, and current moisture state. Key process where NFM interventions act.
4. **Channel routing** — stochastic flow propagation through the river network, with travel time and attenuation parameters. Leaky dams and floodplain reconnection modify these.
5. **Flood state** — river level/flow at key points, with threshold exceedance determining flood damage. The observable output the model must match.

### 2.2 Simulation diagram

```
┌─────────────────────────────────────────────────────────┐
│                  CLIMATE SCENARIO                        │
│  UKCP18 projections perturb rainfall distributions       │
│  RCP2.6 / RCP4.5 / RCP8.5 pathways                     │
└──────────────────┬──────────────────────────────────────┘
                   │ modified rainfall regime
                   ▼
┌─────────────────────────────────────────────────────────┐
│              STOCHASTIC RAINFALL GENERATOR               │
│  Event magnitude, duration, spatial pattern              │
│  Learned from EA rainfall + HadUK-Grid observations      │
│  Perturbed by UKCP18 change factors                      │
└──────────┬──────────────────────────────────────────────┘
           │ rainfall events
           ▼
┌─────────────────────────────────────────────────────────┐
│              SOIL MOISTURE DYNAMICS                       │
│  Antecedent conditions (stochastic, seasonal)            │
│  INTERVENTION: Peat restoration ↑ storage capacity       │
│  INTERVENTION: Woodland planting ↑ interception          │
│               & evapotranspiration                        │
└──────────┬──────────────────────────────────────────────┘
           │ effective rainfall (after losses)
           ▼
┌─────────────────────────────────────────────────────────┐
│             RUNOFF GENERATION                             │
│  Hillslope response (stochastic, land-use dependent)     │
│  INTERVENTION: Woodland ↑ infiltration (up to 60×)       │
│  INTERVENTION: Soil management ↑ permeability            │
│  INTERVENTION: Storage bunds ↑ field-corner retention    │
└──────────┬──────────────────────────────────────────────┘
           │ sub-catchment hydrographs
           ▼
┌─────────────────────────────────────────────────────────┐
│             CHANNEL ROUTING                               │
│  Network flow propagation with stochastic travel times   │
│  INTERVENTION: Leaky dams ↑ attenuation, ↓ peak         │
│  INTERVENTION: Floodplain reconnection ↑ storage         │
│  INTERVENTION: Beaver reintroduction ↑ wetland storage   │
│  Key: spatial placement affects peak synchronisation     │
└──────────┬──────────────────────────────────────────────┘
           │ river flow at gauging stations
           ▼
┌─────────────────────────────────────────────────────────┐
│             FLOOD STATE & DAMAGE                          │
│  Level/flow at critical points vs. threshold             │
│  Properties at risk (from EA risk mapping)               │
│  Expected damage = f(depth, duration, properties)        │
└─────────────────────────────────────────────────────────┘
```

### 2.3 Key modelling choices

- **Lumped sub-catchment approach** initially: divide the catchment into ~10–30 sub-catchments, each with homogeneous land-use and soil characteristics. Not a full 2D hydrodynamic model — that's not the point. The stochadex adds value through stochastic dynamics and policy optimisation, not through spatial hydraulic resolution.
- **Stochastic rainfall generator** learned from observed statistics (fitted distributions of event magnitude, duration, inter-arrival time), perturbed by UKCP18 change factors for future scenarios.
- **NFM interventions as parameter modifiers**: each intervention type changes specific simulation parameters (e.g. leaky dams modify channel attenuation coefficients; woodland changes interception and infiltration rates). The magnitude of the effect is itself uncertain — modelled as a distribution learned from the WWNP evidence base.
- **Time resolution:** hourly for simulation, with sub-daily EA data used for calibration.
- **Ensemble approach:** run hundreds of stochastic realisations per policy scenario to build distributions of flood outcomes.

---

## Phase 3: Learning from Data

### 3.1 Simulation-based inference

The stochadex's established pattern applies directly:

1. **Smooth and aggregate** the EA Hydrology API flow data and rainfall data to produce baseline event rates — "what the catchment does" in response to different rainfall patterns and antecedent conditions.
2. **Fit deviation coefficients** using SBI, matching simulated flow hydrographs at gauging stations to observed data, conditional on the observed rainfall inputs.
3. **Key parameters to learn:**
   - Runoff coefficients by sub-catchment (effective rainfall → runoff)
   - Channel routing velocities and attenuation parameters
   - Soil moisture dynamics (drainage rate, field capacity, saturation threshold)
   - Seasonal variation in evapotranspiration and interception
   - Flood threshold-damage relationship

### 3.2 NFM effectiveness parameters

Rather than hard-coding NFM effects from literature, treat them as uncertain parameters with prior distributions informed by the WWNP evidence:

| Intervention | Effect parameter | Prior (from evidence) |
|-------------|-----------------|----------------------|
| Leaky dams (per cluster) | Peak flow attenuation (%) | 5–15% for events ≤ 1yr RP, declining for larger events |
| Woodland planting (per ha) | Infiltration multiplier | 2–60× depending on soil and maturity (Pontbren) |
| Woodland planting (per ha) | Interception loss (%) | Up to 30% of gross rainfall |
| Floodplain reconnection | Storage volume (m³) | Site-specific, from EA evidence |
| Peat restoration (per ha) | Water table depth change (cm) | 5–20cm from rewetting studies |
| Beaver reintroduction | Peak flow attenuation (%) | Up to 30% in well-established sites (River Otter) |

Where possible, update these priors with data from specific monitored NFM schemes in or near the target catchment.

### 3.3 Validation strategy

- **Temporal holdout:** Train on 2010–2022, validate flood predictions on 2023–2025 events.
- **Event holdout:** Withhold specific major flood events (e.g. Boxing Day 2015 if using Yorkshire catchments) from training, test whether the model reproduces observed peak flows.
- **Cross-catchment:** Check whether parameters transfer to an adjacent catchment with similar characteristics.
- **NFM validation:** Where monitored before/after data exists for an NFM scheme within the catchment, check whether the model's predicted effect is consistent with observed change.

---

## Phase 4: Decision Science Layer

### 4.1 Intervention actions to evaluate

The decision science layer evaluates portfolios of NFM interventions — the analogue of rugby substitution timing:

| Intervention | Where it acts in the model | Decision variables |
|-------------|---------------------------|-------------------|
| **Leaky dam clusters** | Channel routing attenuation | Number, location (which sub-catchments), density |
| **Woodland planting** | Interception, infiltration, evapotranspiration | Area (ha), location, species mix (maturity timeline) |
| **Floodplain reconnection** | Off-channel storage volume | Location, area reconnected |
| **Storage bunds** | Field-corner runoff detention | Number, location, capacity |
| **Peat restoration** | Headwater soil moisture storage | Area, rewetting intensity |
| **Beaver reintroduction** | Headwater wetland creation | Location (which tributaries) |

### 4.2 The spatial placement problem

A crucial insight from the NFM literature is that spatial targeting of interventions matters enormously. Poorly placed NFM can *synchronise* rather than *desynchronise* flood peaks from tributaries, potentially increasing downstream flood risk. The stochadex's ensemble simulation approach is ideally suited to exploring this — run thousands of realisations with different placement strategies and evaluate the distribution of downstream outcomes.

### 4.3 Objective function

For each intervention portfolio, simulate multiple trajectories across stochastic rainfall scenarios and evaluate:

- **Primary outcome:** Expected annual damage (EAD) at the downstream flood-risk community, under current climate and UKCP18 scenarios at 2040 and 2070
- **Constraint:** Total intervention cost ≤ budget (using EA standard cost estimates for NFM measures)
- **Secondary outcomes:** Number of properties protected, reduction in peak flow at key gauging points, co-benefits (carbon sequestration from woodland, biodiversity uplift)
- **Robustness metric:** Performance across climate scenarios (does the portfolio work under RCP2.6 and RCP8.5?)

### 4.4 Output

For each catchment, produce actionable recommendations:

> *"For the Upper Calder catchment under a £2M budget, a portfolio of 40 leaky dam clusters in tributaries X and Y combined with 50ha of riparian woodland in sub-catchments A and B reduces expected annual damage by 35% under RCP4.5, with the woodland component taking 15 years to reach full effectiveness. This portfolio is robust across emission scenarios, with EAD reduction ranging from 28% (RCP8.5) to 42% (RCP2.6). Concentrating all investment in leaky dams alone would yield only 18% EAD reduction due to declining effectiveness for larger events."*

---

## Phase 5: Extensions

Once the core single-catchment model is validated:

1. **Multi-catchment library:** Repeat for 5–10 UK catchments with different characteristics (upland/lowland, urban/rural, permeable/impermeable) to build a library of transferable insights
2. **Grey + green integration:** Add engineered defences (walls, embankments, reservoirs) alongside NFM to evaluate hybrid portfolios — councils need to compare and combine both
3. **Dynamic adaptation pathways:** Instead of fixed portfolios, model sequential decision-making — "plant woodland now, add leaky dams in year 5 if resistance trends are adverse" — using the stochadex's temporal decision framework
4. **Insurance integration:** Connect flood damage outputs to insurance loss models, enabling re/insurers to price the value of NFM investments
5. **Real-time operational mode:** Connect to live EA flood monitoring API for real-time reservoir operation or temporary barrier deployment decisions during events
6. **Community engagement tool:** Build a simplified interactive front-end where local flood groups can explore "what if we planted woodland here?" scenarios for their own catchment

---

## Concrete First Steps

### Week 1–2: Data acquisition and exploration

- [ ] Identify target catchment and list all EA gauging stations within it
- [ ] Pull 15-min and daily flow data from EA Hydrology API for those stations (2010–2025)
- [ ] Pull co-located rainfall station data from EA Rainfall API
- [ ] Download HadUK-Grid daily rainfall for the catchment area
- [ ] Download EA Risk of Flooding from Rivers and Sea data for the catchment
- [ ] Exploratory analysis: characterise the rainfall-runoff relationship, identify major flood events, estimate empirical flood frequency curves

### Week 3–4: Minimal stochadex simulation

- [ ] Implement a lumped rainfall-runoff simulation for one sub-catchment in the stochadex
- [ ] Define state transitions: rainfall → soil moisture → runoff → channel flow → flood state
- [ ] Implement a simple stochastic rainfall generator (fitted to observed event statistics)
- [ ] Verify the simulation produces qualitatively sensible hydrographs with hand-tuned parameters

### Week 5–6: Simulation-based inference

- [ ] Smooth and aggregate observed rainfall-flow pairs into baseline response functions
- [ ] Set up SBI to learn runoff and routing parameters from observed data
- [ ] Validate: does the fitted model reproduce held-out flood events?
- [ ] Extend to multiple sub-catchments with channel routing between them

### Week 7–8: NFM interventions and decision science

- [ ] Add NFM intervention parameter modifiers (leaky dams, woodland) with uncertain effectiveness priors from WWNP evidence
- [ ] Implement 3–4 candidate intervention portfolios as action sets
- [ ] Perturb rainfall with UKCP18 change factors for 2040 and 2070 scenarios
- [ ] Run policy evaluation: simulate ensembles under each portfolio × climate scenario
- [ ] Produce initial findings and visualisations
- [ ] Write up as a blog post in the "Engineering Smart Actions in Practice" series

---

## Key Data Sources Summary

| Source | URL | Data type | Access |
|--------|-----|-----------|--------|
| EA Hydrology API | environment.data.gov.uk/hydrology/ | River flow, level, groundwater, rainfall — sub-daily, quality-checked archive | Free REST API, Open Government Licence |
| EA Flood Monitoring API | environment.data.gov.uk/flood-monitoring/ | Real-time flood warnings, levels, flows, 3-day risk forecast | Free REST API, Open Government Licence |
| EA Rainfall API | api.gov.uk/ea/ (part of 10-API catalogue) | Station rainfall time series | Free REST API, Open Government Licence |
| NRFA | nrfa.ceh.ac.uk | UK river flow records, peak flow dataset, 1,600+ stations | Free download |
| HadUK-Grid / UKCP18 | climate-themetoffice.hub.arcgis.com | Gridded observations + climate projections (probabilistic, 12km, 2.2km) | Registration required (UKCP UI) or CEDA Archive |
| UKCP18 bias-corrected | crudata.uea.ac.uk/cru/data/ukcp/ukcp18bc.htm | Bias-corrected precipitation and temperature, 12km | Open Database Licence |
| UKCP18 CPM rainfall events | catalogue.ceh.ac.uk (DOI: 10.5285/0d786a81...) | ~72,000 widespread rainfall event summaries, 3 time periods | Open Government Licence |
| EA Risk of Flooding | data.gov.uk (search "Risk of Flooding from Rivers and Sea") | Flood likelihood maps, postcode-level risk | Open Government Licence |
| EA Catchment Data API | api.gov.uk/ea/ | Catchment boundaries, characteristics | Free REST API |
| EA WWNP Evidence Directory | gov.uk (search "Working with Natural Processes") | 700+ studies on NFM effectiveness | Free PDF |

---

## References and Related Work

- EA "Working with Natural Processes" evidence directory — comprehensive NFM effectiveness evidence including river/floodplain management guidance (updated February 2025)
- Leaky dam quantification study — transfer function noise modelling for 50 upland storm events showing ~10% peak reduction for ≤1yr return period (ScienceDirect, 2023)
- Pickering "Slowing the Flow" — combined riparian woodland, leaky structures, and bund reduced peak flows by 15–20%; cost £500k vs. millions for hard defences
- Wildlife Trusts NFM research — 10 schemes averaged 4:1 cost-benefit over 10 years, 10:1 over 30 years
- River Otter beaver trial — beaver dams attenuated flood flows by ~30% on average, even in high flow conditions
- UKCP18 Local (2.2km) projections — convection-permitting model capturing extreme rainfall events missed by coarser models
- LFPtools — open-source Python toolbox for LISFLOOD-FP flood model preparation, validated on the Severn
- HEC-RAS, HEC-HMS — US Army Corps open-source hydraulic and hydrologic modelling (useful for comparison/validation, not the core simulation approach here)