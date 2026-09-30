// picker scoring, js twin of the go engine scorer
// reads window.PICKER_GEN-shaped data only: weights, gates, packed mu/syn strings
// dual env: plain <script> global for the picker page, module.exports for node tests
(function (root) {
'use strict';

function parsePacked(s) {
  if (!s) return [];
  return s.split(',').filter(Boolean).map(cell => {
    const p = cell.split(':');
    return { idx: +p[0], pct: +p[1], conf: p.length > 2 ? +p[2] : 0 };
  });
}

function mean(xs) {
  return xs.reduce((s, x) => s + x, 0) / xs.length;
}

function stdev(xs) {
  if (!xs.length) return 0;
  const m = mean(xs);
  return Math.sqrt(xs.reduce((s, x) => s + (x - m) * (x - m), 0) / xs.length);
}

// pop-weighted mean, plain mean when total popularity weight is 0, 0 when empty
function popWeightedMean(vals, ws) {
  if (!vals.length) return 0;
  let num = 0, den = 0;
  for (let i = 0; i < vals.length; i++) { num += ws[i] * vals[i]; den += ws[i]; }
  return den === 0 ? mean(vals) : num / den;
}

function cache(G, key, build) {
  if (!G.__psCache) G.__psCache = {};
  if (!G.__psCache[key]) G.__psCache[key] = build();
  return G.__psCache[key];
}

const muRows = G => cache(G, 'mu', () => G.mu.map(parsePacked));
const synRows = G => cache(G, 'syn', () => G.syn.map(parsePacked));
const slugIdxMap = G => cache(G, 'slugIdx', () => {
  const m = {};
  G.slugs.forEach((s, i) => { m[s] = i; });
  return m;
});
// score consts live in the generated data and the generator always emits
// them, so a payload without them is corrupt: fail loud rather than score
// on a duplicated stale copy of the hub values
const consts = G => cache(G, 'scoreConsts', () => {
  if (!G.scoreConsts) throw new Error('picker data payload missing scoreConsts');
  return G.scoreConsts;
});

// the syn phase is weighted by the role being picked (supports pair with the
// picked lane partner, cores factor pairs less), authored in the hub's
// weights.synByRole. same corruption rule as scoreConsts: fail loud
const synWeight = (G, role) => {
  const byRole = G.weights && G.weights.synByRole;
  if (!byRole) throw new Error('picker data payload missing synByRole');
  const w = byRole[role];
  if (w === undefined) throw new Error('picker data payload synByRole has no role ' + role);
  return w;
};

// candidate field of a role: the seat plus the other seats of its shared
// pool (hub sharedRolePools), mirroring the go twin's Config.RoleField.
// candidate membership and rule scoping both key on it
function roleField(G, role) {
  const groups = G.sharedRolePools || [];
  for (const g of groups) {
    if (g.includes(role)) return g;
  }
  return [role];
}

function playsInField(roles, field) {
  return (roles || []).some(r => field.includes(r));
}

// a missing packed cell reads 0, the same as a map miss in the go twin
function pctAt(rows, pos, idx) {
  const hit = rows[pos].find(r => r.idx === idx);
  return hit ? hit.pct : 0;
}

// condition vocabulary, all data from the gates object, nothing hardcoded.
// omitted numeric fields read 0 and empty hero lists read false, mirroring
// the go twin's zero values so malformed shapes cannot diverge between twins
function condHolds(c, ctx) {
  const idx = h => ctx.slugIdx[h];
  const heroes = c.heroes || [];
  switch (c.kind) {
    case 'enemyVisible': return heroes.length > 0 && heroes.every(h => ctx.enemies.has(idx(h)));
    case 'enemyVisibleAny': return heroes.some(h => ctx.enemies.has(idx(h)));
    case 'enemyVisibleNone': return !heroes.some(h => ctx.enemies.has(idx(h)));
    case 'enemyVisibleAnyCountMin': return heroes.filter(h => ctx.enemies.has(idx(h))).length >= (c.count || 0);
    case 'allyVisibleAny': return heroes.some(h => ctx.allies.has(idx(h)));
    case 'enemyCountMin': return ctx.enemyN >= (c.count || 0);
    case 'enemyCountMax': return ctx.enemyN <= (c.count || 0);
    case 'enemyAoEMax': return ctx.aoeN <= (c.max || 0);
    case 'enemyAoEMin': return ctx.aoeN >= (c.min || 0);
    case 'minEnemyCount': return ctx.enemyN >= (c.minEnemyCount || 0);
    case 'roleCandidatesGated': return ctx.roleGatedN() >= (c.count || 0);
    default: return false;
  }
}

// evaluate all rules for one candidate, in authored order, actions merge into a result
function gateResult(rules, ctx, role, cIdx) {
  const res = { hardGated: false, notes: [], firedRuleIds: [], gateDelta: 0 };
  for (const r of rules) {
    if (ctx.slugIdx[r.target] !== cIdx) continue;
    if (r.roles && r.roles.length && !r.roles.some(x => ctx.roleField.includes(x))) continue;
    const when = r.when || [];
    if (!when.every(c => condHolds(c, ctx))) continue;
    res.firedRuleIds.push(r.id);
    if (r.note) res.notes.push(r.note);
    if (r.action === 'hard_gate') res.hardGated = true;
    else if (r.action === 'bonus') res.gateDelta += ctx.gateDeltas.bonus || 0;
    else if (r.action === 'penalty') res.gateDelta += ctx.gateDeltas.penalty || 0;
  }
  return res;
}

// gated count for roleCandidatesGated: field candidates that are hard gated
// under this state, with roleCandidatesGated conditions inert to avoid recursion
function gatedCount(G, rules, role, baseCtx) {
  const field = roleField(G, role);
  let n = 0;
  for (let p = 0; p < G.poolIdx.length; p++) {
    const entry = G.entries[p] || { roles: [], tier: {} };
    if (!playsInField(entry.roles, field)) continue;
    const r = gateResult(rules, baseCtx, role, G.poolIdx[p]);
    if (r.hardGated) n++;
  }
  return n;
}

function memoOnce(fn) {
  let memo = null;
  return () => { if (memo === null) memo = fn(); return memo; };
}

// the inert view treats roleCandidatesGated as never holding (-Infinity >= count is
// false), so the baseline count cannot recurse through the condition
function makeCtx(G, state, role) {
  const rules = ((G.gates && G.gates.rules) || []).concat((G.gates && G.gates.autogenerated) || []);
  const slugIdx = slugIdxMap(G);
  const aoeSet = new Set((G.gates && G.gates.aoeClearHeroes) || []);
  const inert = {
    slugIdx,
    roleField: roleField(G, role),
    enemies: new Set(state.enemies),
    allies: new Set(state.allies),
    enemyN: state.enemies.length,
    aoeN: state.enemies.filter(i => aoeSet.has(G.slugs[i])).length,
    gateDeltas: (G.gates && G.gates.gateDeltas) || {},
    roleGatedN: () => -Infinity,
  };
  const ctx = Object.assign({}, inert, {
    roleGatedN: memoOnce(() => gatedCount(G, rules, role, inert)),
  });
  return { rules, ctx };
}

function scoreCandidate(G, state, role, candidateIdx) {
  const pos = G.poolIdx.indexOf(candidateIdx);
  if (pos < 0) throw new Error('candidate not in pool: ' + candidateIdx);
  const W = G.weights;
  const taken = new Set(state.allies.concat(state.enemies, state.banned));
  const a = state.allies.length, k = state.enemies.length;
  const C = consts(G);
  const muP = muRows(G), synP = synRows(G);
  const muOf = i => pctAt(muP, pos, i);
  const synOf = i => pctAt(synP, pos, i);

  const knownMu = k === 0 ? 0 : mean(state.enemies.map(muOf));
  const knownSyn = a === 0 ? 0 : mean(state.allies.map(synOf));
  const priorV = G.prior[pos];

  const unseen = [];
  for (let i = 0; i < G.rosterCount; i++) if (!taken.has(i) && i !== candidateIdx) unseen.push(i);
  const unseenEnemies = unseen.filter(i => !state.enemies.includes(i));
  const genericFit = popWeightedMean(unseen.map(synOf), unseen.map(i => G.pop[i]));
  const exposureRaw = popWeightedMean(
    unseenEnemies.map(i => Math.max(0, C.midPct - muOf(i))),
    unseenEnemies.map(i => G.pop[i]));
  const flexEnemies = unseenEnemies.slice()
    .sort((x, y) => G.pop[y] - G.pop[x] || x - y).slice(0, C.flexCap);
  const flexibility = -stdev(flexEnemies.map(muOf)) / C.flexHalf;

  const { rules, ctx } = makeCtx(G, state, role);
  const gate = gateResult(rules, ctx, role, candidateIdx);
  const gateDelta = gate.gateDelta;

  // accumulation order mirrors the go scorer exactly for bit-level parity
  const score = W.knownMu * (k / C.enemySlots) * knownMu
    + synWeight(G, role) * (a / C.enemySlots) * knownSyn
    + W.prior * priorV
    + W.genericFit * ((C.allySlots - a) / C.allySlots) * genericFit
    - W.exposure * ((C.enemySlots - k) / C.enemySlots) * exposureRaw
    + W.flexibility * flexibility
    + gateDelta;

  return {
    score,
    terms: { knownMu, knownSyn, prior: priorV, genericFit, exposure: exposureRaw, flexibility, gateDelta },
    hardGated: gate.hardGated,
    notes: gate.notes,
    firedRuleIds: gate.firedRuleIds,
  };
}

function rankRole(G, state, role) {
  const taken = new Set(state.allies.concat(state.enemies, state.banned));
  const field = roleField(G, role);
  const rows = [];
  for (let p = 0; p < G.poolIdx.length; p++) {
    const idx = G.poolIdx[p];
    if (taken.has(idx)) continue;
    const entry = G.entries[p] || { roles: [], tier: {} };
    if (!playsInField(entry.roles, field)) continue;
    rows.push(Object.assign({ idx }, scoreCandidate(G, state, role, idx)));
  }
  rows.sort((x, y) => y.score - x.score || x.idx - y.idx);
  return rows;
}

const PickerScore = { parsePacked, scoreCandidate, rankRole };
if (typeof module === 'object' && module.exports) module.exports = PickerScore;
else root.PickerScore = PickerScore;
})(typeof self !== 'undefined' ? self : this);
