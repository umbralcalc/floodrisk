package catchment

import (
	"math/rand/v2"
)

// InterventionType identifies the kind of NFM intervention.
type InterventionType int

const (
	LeakyDams InterventionType = iota
	WoodlandPlanting
	FloodplainReconnection
	PeatRestoration
)

// String returns a human-readable name for the intervention type.
func (t InterventionType) String() string {
	switch t {
	case LeakyDams:
		return "leaky_dams"
	case WoodlandPlanting:
		return "woodland_planting"
	case FloodplainReconnection:
		return "floodplain_reconnection"
	case PeatRestoration:
		return "peat_restoration"
	default:
		return "unknown"
	}
}

// Intervention describes a single NFM intervention placed in a specific
// sub-catchment. The effectiveness parameters are drawn from prior
// distributions informed by the EA WWNP evidence base.
type Intervention struct {
	Type         InterventionType
	SubCatchment string  // which sub-catchment this is placed in
	Scale        float64 // intensity: number of dam clusters, hectares of woodland, etc.
}

// InterventionEffectiveness holds sampled effectiveness values for a
// single intervention. These are drawn from uncertain priors each
// ensemble member, propagating uncertainty in NFM effectiveness.
type InterventionEffectiveness struct {
	// FieldCapacityIncreaseMM is the additive increase to field capacity
	// from improved soil storage (woodland, peat restoration).
	FieldCapacityIncreaseMM float64

	// ETRateIncreaseMM is the additive increase to ET rate from
	// interception by woodland canopy (mm/day).
	ETRateIncreaseMM float64

	// RoutingCoefficientReduction is the multiplicative factor applied
	// to routing coefficients (0–1, where 0.8 means 20% more attenuation).
	// Applies to leaky dams and floodplain reconnection.
	RoutingCoefficientReduction float64
}

// InterventionPriors defines the prior distribution parameters for each
// intervention type's effectiveness, based on WWNP evidence.
//
// Routing reductions use a linear-with-cap model: the total reduction
// in a sub-catchment is Scale/FullScale * sampled_max_reduction, capped
// at the sampled maximum. This avoids unrealistic compound effects from
// multiplicative per-unit application.
type InterventionPriors struct {
	// LeakyDams: total effect for a fully-deployed scheme in one sub-catchment.
	// Evidence: ~10% peak reduction for ≤1yr return period events (WWNP),
	// declining for larger events. A "full deployment" is ~20 clusters.
	// Scale represents number of clusters; FullScale is the reference
	// deployment size at which the max reduction is achieved.
	LeakyDamRoutingReductionMin float64 // min total fraction reduction at full scale
	LeakyDamRoutingReductionMax float64 // max total fraction reduction at full scale
	LeakyDamFullScale           float64 // number of clusters for full effect

	// Woodland: per 10 hectares planted.
	// Evidence: infiltration 2–60x increase (Pontbren), interception
	// up to 30% of gross rainfall, but takes 15+ years for full effect.
	// Effect: increases field capacity and ET rate.
	WoodlandFieldCapacityMin float64 // min mm increase per 10ha
	WoodlandFieldCapacityMax float64 // max mm increase per 10ha
	WoodlandETRateMin        float64 // min mm/day increase per 10ha
	WoodlandETRateMax        float64 // max mm/day increase per 10ha

	// Floodplain reconnection: total effect per site.
	// Evidence: site-specific storage volumes (EA evidence).
	// Effect: reduces routing coefficient (off-channel storage delays flow).
	FloodplainRoutingReductionMin float64 // min reduction per site
	FloodplainRoutingReductionMax float64 // max reduction per site
	FloodplainMaxSites            float64 // max sites before diminishing returns

	// Peat restoration: per 10 hectares restored.
	// Evidence: 5–20cm water table depth change from rewetting studies.
	// Effect: increases field capacity (more headwater storage).
	PeatFieldCapacityMin float64 // min mm increase per 10ha
	PeatFieldCapacityMax float64 // max mm increase per 10ha
}

// DefaultInterventionPriors returns evidence-based prior ranges from WWNP.
func DefaultInterventionPriors() InterventionPriors {
	return InterventionPriors{
		// Leaky dams: 5–15% total routing reduction at full deployment
		// (~20 clusters). Fewer clusters give proportionally less effect.
		LeakyDamRoutingReductionMin: 0.05,
		LeakyDamRoutingReductionMax: 0.15,
		LeakyDamFullScale:           20.0,

		// Woodland: per 10ha — moderate field capacity and ET increase.
		WoodlandFieldCapacityMin: 5.0,  // mm
		WoodlandFieldCapacityMax: 30.0, // mm
		WoodlandETRateMin:        0.1,  // mm/day
		WoodlandETRateMax:        0.5,  // mm/day

		// Floodplain reconnection: 5–15% routing reduction per site,
		// up to 3 sites before diminishing returns.
		FloodplainRoutingReductionMin: 0.05,
		FloodplainRoutingReductionMax: 0.15,
		FloodplainMaxSites:            3.0,

		// Peat restoration: per 10ha — increases headwater storage.
		PeatFieldCapacityMin: 10.0, // mm
		PeatFieldCapacityMax: 40.0, // mm
	}
}

// SampleEffectiveness draws an effectiveness value for a single
// intervention from the prior distributions. Scale is applied
// linearly (e.g. 3 clusters of leaky dams = 3x the per-cluster effect).
func SampleEffectiveness(
	intervention Intervention,
	priors InterventionPriors,
	rng *rand.Rand,
) InterventionEffectiveness {
	uniform := func(lo, hi float64) float64 {
		return lo + rng.Float64()*(hi-lo)
	}

	eff := InterventionEffectiveness{
		RoutingCoefficientReduction: 1.0, // no change by default
	}

	switch intervention.Type {
	case LeakyDams:
		// Linear scaling: fraction of full deployment determines fraction
		// of max effect. Capped at the sampled maximum.
		maxReduction := uniform(
			priors.LeakyDamRoutingReductionMin,
			priors.LeakyDamRoutingReductionMax,
		)
		fraction := intervention.Scale / priors.LeakyDamFullScale
		if fraction > 1.0 {
			fraction = 1.0
		}
		eff.RoutingCoefficientReduction = 1.0 - maxReduction*fraction

	case WoodlandPlanting:
		// Scale is in units of 10ha.
		units := intervention.Scale / 10.0
		eff.FieldCapacityIncreaseMM = uniform(
			priors.WoodlandFieldCapacityMin,
			priors.WoodlandFieldCapacityMax,
		) * units
		eff.ETRateIncreaseMM = uniform(
			priors.WoodlandETRateMin,
			priors.WoodlandETRateMax,
		) * units

	case FloodplainReconnection:
		// Linear scaling with diminishing returns past max sites.
		maxReduction := uniform(
			priors.FloodplainRoutingReductionMin,
			priors.FloodplainRoutingReductionMax,
		)
		fraction := intervention.Scale / priors.FloodplainMaxSites
		if fraction > 1.0 {
			fraction = 1.0
		}
		eff.RoutingCoefficientReduction = 1.0 - maxReduction*fraction

	case PeatRestoration:
		// Scale is in units of 10ha.
		units := intervention.Scale / 10.0
		eff.FieldCapacityIncreaseMM = uniform(
			priors.PeatFieldCapacityMin,
			priors.PeatFieldCapacityMax,
		) * units
	}

	return eff
}

// Portfolio is a named collection of interventions.
type Portfolio struct {
	Name           string
	Interventions  []Intervention
	CostGBP        float64 // estimated total cost
}

// ApplyPortfolio modifies model parameters and routing coefficients
// according to the sampled effectiveness of each intervention in the
// portfolio. It returns new copies of the params and routing coefficients
// (does not mutate the originals).
//
// runoffParams are per-sub-catchment (map[subcatchment]map[param][]float64),
// routingCoeffs are indexed by sub-catchment order in the config.
func ApplyPortfolio(
	portfolio Portfolio,
	baseRunoffParams map[string][]float64,
	baseRoutingCoeffs []float64,
	subCatchments []string,
	priors InterventionPriors,
	rng *rand.Rand,
) (modifiedParams map[string][]float64, modifiedRouting []float64) {
	// Deep-copy base params.
	modifiedParams = make(map[string][]float64, len(baseRunoffParams))
	for k, v := range baseRunoffParams {
		cp := make([]float64, len(v))
		copy(cp, v)
		modifiedParams[k] = cp
	}

	// Deep-copy routing coefficients.
	modifiedRouting = make([]float64, len(baseRoutingCoeffs))
	copy(modifiedRouting, baseRoutingCoeffs)

	// Build sub-catchment index lookup.
	scIndex := make(map[string]int, len(subCatchments))
	for i, name := range subCatchments {
		scIndex[name] = i
	}

	// Aggregate effects per sub-catchment.
	type aggregatedEffect struct {
		fieldCapacityAdd float64
		etRateAdd        float64
		routingFactor    float64 // multiplicative
	}
	effects := make(map[string]*aggregatedEffect)
	for _, name := range subCatchments {
		effects[name] = &aggregatedEffect{routingFactor: 1.0}
	}

	for _, intervention := range portfolio.Interventions {
		eff := SampleEffectiveness(intervention, priors, rng)
		sc := intervention.SubCatchment
		agg, ok := effects[sc]
		if !ok {
			continue // sub-catchment not in model
		}
		agg.fieldCapacityAdd += eff.FieldCapacityIncreaseMM
		agg.etRateAdd += eff.ETRateIncreaseMM
		agg.routingFactor *= eff.RoutingCoefficientReduction
	}

	// Apply aggregated effects to shared params (additive for FC/ET)
	// and per-sub-catchment routing coefficients.
	totalFCAdd := 0.0
	totalETAdd := 0.0
	for _, agg := range effects {
		totalFCAdd += agg.fieldCapacityAdd
		totalETAdd += agg.etRateAdd
	}
	// Shared PDM params get the average effect across sub-catchments
	// (since they're shared, not per-sub-catchment).
	nSC := float64(len(subCatchments))
	if nSC > 0 {
		modifiedParams["field_capacity"] = []float64{
			baseRunoffParams["field_capacity"][0] + totalFCAdd/nSC,
		}
		modifiedParams["et_rate"] = []float64{
			baseRunoffParams["et_rate"][0] + totalETAdd/nSC,
		}
	}

	// Routing coefficients are per-sub-catchment.
	for name, agg := range effects {
		idx, ok := scIndex[name]
		if !ok || idx >= len(modifiedRouting) {
			continue
		}
		// Reduce routing coefficient (more attenuation).
		modifiedRouting[idx] *= agg.routingFactor
		// Clamp to [0.01, 1.0].
		if modifiedRouting[idx] < 0.01 {
			modifiedRouting[idx] = 0.01
		}
	}

	return modifiedParams, modifiedRouting
}
