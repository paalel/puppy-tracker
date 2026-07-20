# Poop Prediction Model

This document describes the statistical model used to estimate the probability that the dog will poop during a given wake window. The goal is to surface a running probability on the current (and upcoming) wake windows so we know when to prioritise an outdoor toilet break.

---

## What we are trying to predict

For each wake window, we want to estimate the probability that she will poop, given that she has not yet pooped since the last observed poop. This is a **discrete-time hazard model**: each wake window is a discrete "opportunity" for the event (poop) to occur, and the model conditions on the event not yet having happened. This framing is common in survival analysis and is well-suited to this problem.

More precisely, the model estimates:

> P(poop in window t | no poop in windows 1, …, t−1 since last poop)

Standard logistic regression applied window-by-window is a valid and well-known way to fit this kind of model. In fact, the joint likelihood for a sequence of non-events followed by an event exactly factors into a product of independent Bernoulli terms — so treating each wake window as an independent row in logistic regression is **mathematically exact** for maximum likelihood estimation in a discrete-time hazard model, not an approximation.

---

## Training data

Each completed, non-excluded wake session produces one row:

| Field | Description |
|---|---|
| `local_hour` | Local clock hour of `woke_at` for that session |
| `hours_since_poop` | Hours elapsed between the most recent prior poop event and this session's `woke_at` |
| `poop` | 1 if she pooped during this session, 0 otherwise |

Rows are excluded if:
- The session is marked `excluded = 1` (e.g. vet trips, unusual days)
- There is no prior poop on record yet (model cannot compute `hours_since_poop`)
- `hours_since_poop ≤ 0` (data integrity guard)

Night toilet walks (recorded separately when she is taken out mid-night while sleeping) are **not** included in training data and **not** counted as resetting `hours_since_poop`. This means the model has no awareness of what happens overnight.

The model is refit from scratch in memory every time a session transitions (wake → crate → sleep), so it always reflects all observations to date.

---

## Features

We use 6 features per observation:

| Index | Feature | Rationale |
|---|---|---|
| 0 | Intercept (1) | Bias term |
| 1 | sin(2π · local_hour / 24) | First harmonic of time of day (fundamental) |
| 2 | cos(2π · local_hour / 24) | First harmonic of time of day (fundamental) |
| 3 | sin(4π · local_hour / 24) | Second harmonic of time of day |
| 4 | cos(4π · local_hour / 24) | Second harmonic of time of day |
| 5 | log(1 + max(0, hours_since_poop − 6)) | Time pressure after the refractory period |

**Time of day** is encoded using the first two Fourier harmonics so that midnight and 23:00 are treated as adjacent. The first harmonic (indices 1–2) captures a single daily peak; the second harmonic (indices 3–4) allows a second peak — for example, a morning peak and a post-dinner peak — without assuming the shape in advance. L2 regularisation will suppress the second harmonic if the data does not support it.

Hours are taken from the **local clock** (Norway: CEST = UTC+2 in summer, CET = UTC+1 in winter). Using UTC would shift all observations by the timezone offset and cause inconsistency across daylight saving transitions.

**Hours since last poop** is shifted by 6 hours before applying the log transform: `log(1 + max(0, hours − 6))`. This produces zero urgency for the first 6 hours (the refractory / digestive transit period) and then grows logarithmically. The 6-hour shift is supported by the observed data:

| Hours since poop | Sessions | Poop rate |
|---|---|---|
| 0–4h | 25 | 12% |
| 4–8h | 30 | 13% |
| 8–12h | 36 | 39% |
| 12–16h | 17 | 65% |
| 16h+ | 21 | 43% |

Poop rate is flat and low for the first 8 hours, consistent with a digestive transit time of 6–8 hours, then rises sharply. The plain `log(1+x)` transform is highest-sensitivity near zero, which is the wrong direction.

---

## Model

We fit an **L2-regularised logistic regression** using Iteratively Reweighted Least Squares (IRLS), which is equivalent to Newton-Raphson on the log-likelihood.

### Objective

Maximise the penalised log-likelihood:

```
ℓ(β) = Σᵢ [yᵢ log μᵢ + (1−yᵢ) log(1−μᵢ)] − (λ/2) Σⱼ₌₁⁵ βⱼ²
```

where μᵢ = σ(xᵢᵀβ) is the sigmoid of the linear predictor, and λ = 1.0 is the L2 penalty. The intercept (j=0) is **not** penalised.

### IRLS update

Each Newton step solves:

```
H δ = g
β ← β + δ
```

where:
- **H** = XᵀWX + λI (Hessian, with L2 added to non-intercept diagonal)
- **g** = Xᵀ(y − μ) − λβ (gradient)
- **W** = diag(μᵢ(1−μᵢ)) (working weights, floored at 1e-10)

The algorithm runs for up to 50 iterations and stops early when the maximum coefficient change falls below 1e-8.

### Regularisation

λ = 1.0 is a heuristic default and has not been tuned via cross-validation. With a small and growing dataset, the penalty helps prevent overfitting to noise in the early data.

---

## Uncertainty quantification

After fitting, we compute an **80% confidence interval** using the delta method:

1. Approximate covariance of β̂ as the inverse Hessian H⁻¹ at convergence.
2. For a new point x, the variance of the linear predictor is: Var(xᵀβ̂) = xᵀ H⁻¹ x
3. Apply ±1.28 standard deviations on the linear scale, then push through the sigmoid:

```
lo = σ(xᵀβ̂ − 1.28 · √(xᵀH⁻¹x))
hi = σ(xᵀβ̂ + 1.28 · √(xᵀH⁻¹x))
```

The delta method assumes the asymptotic normality of β̂. With small samples this approximation may be poor, and the intervals should be treated as indicative rather than precise.

**Important:** these intervals describe uncertainty in the *estimated average probability* — how confident we are in our β̂. They do not describe the outcome uncertainty for a single wake window. Even if the interval is narrow, the dog still either poops or doesn't: a 70% probability with a tight CI still means a 30% chance of no poop. The intervals are not predictive intervals for individual events.

---

## Prediction at runtime

When the app loads today's view, it predicts for:

- **Current (active) session**: uses actual `woke_at` local hour and the true `hours_since_poop` at this moment.
- **Future sessions**: each future session advances `hours_since_poop` by one estimated cycle (awake minutes + nap minutes, in hours), compounding forward.

---

## Assumptions

1. **Two Fourier harmonics are sufficient.** We model the first two harmonics of the daily cycle, which allows up to two peaks per day (e.g. a morning peak and an evening post-meal peak). Higher harmonics are excluded; L2 regularisation will suppress the second harmonic if the data does not support it.

2. **A 6-hour shift captures the refractory period.** The feature `log(1 + max(0, hours − 6))` assumes urgency is effectively zero for the first 6 hours after a poop. The data shows a flat ~12–13% rate for 0–8 hours, so the shift is empirically motivated but the exact cutoff is approximate.

3. **Observations are independent.** In a discrete-time hazard model, the joint likelihood for a waiting-time sequence (0, 0, …, 1) exactly factors into a product of independent Bernoulli terms. Treating each wake window as an independent row in logistic regression is therefore **mathematically exact**, not an approximation. This is the standard justification for fitting discrete-time hazard models via ordinary logistic regression.

4. **Stationarity.** The model assumes the underlying poop distribution is stable and weights all historical observations equally. As the dog ages or the routine changes, older observations may become less representative. A natural improvement would be exponential time-decay weighting (`w_i = e^(−α × days_ago)`), which would cause the model to prioritise recent behaviour without discarding history entirely.

5. **Night toilets are not modelled.** Night toilet tracking has been removed from the app as she reliably sleeps through the night. The `night_toilets` table remains in the database for historical data but is no longer written to or read by the model.

6. **All non-excluded sessions are equally informative.** Unusual-but-included days (e.g. days with lots of extra treats or a particularly exciting environmental session) receive equal weight.

---

## Data we are not currently using

These are signals that plausibly affect poop timing but are not in the model:

| Signal | Notes |
|---|---|
| **Meal timing** | She gets one large meal per day, typically between 17:00 and 19:00 local time depending on when she wakes up. The gastrocolic reflex (stomach distension triggering colon emptying within 30–60 minutes of eating) and the 6–8 hour digestive transit time together likely explain much of the time-of-day signal. Currently captured only implicitly via the Fourier features. A direct feature such as `hours_since_meal` would likely carry more predictive weight than clock time alone. |
| **Snack timing** | Small treats given during training sessions and a licking mat with frozen yoghurt to cool down. These contribute to total intake but timing is not tracked. |
| **Calm wind-down snacks** | Sessions with calm wind-down enabled typically include a snack to help her relax before crating. This is a small but regular food event that is not tracked separately. |
| **Physical activity level** | Logged per session (yes/no) but not used as a feature. High physical activity likely speeds up digestion. |
| **Environmental activity** | Logged per session (yes/no). Novel environments (vet, park) may cause stress-related urgency. |
| **Night toilet poops** | Night toilet tracking removed from the app. Historical data remains in the database but is no longer used. |
| **Hydration** | Not tracked. Affects stool consistency and possibly timing. |
| **Age / developmental stage** | Not included. Puppy bowel control improves significantly over the first 6–12 months. |
| **Training quality / overtired flag** | Logged per session but not used. Overtired sessions might correlate with higher stress and therefore urgency. |
