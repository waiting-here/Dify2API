"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const activity = require("./static/admin_activity.js");

test("activity ranges are inclusive UTC days", () => {
  const now = new Date("2026-08-09T23:59:59-04:00");
  assert.deepEqual(activity.RANGE_DAYS, [7, 28, 90, 400]);
  assert.deepEqual(activity.rangeForDays(7, now), { since: "2026-08-04", until: "2026-08-10" });
  assert.deepEqual(activity.rangeForDays(400, new Date("2026-08-09T00:01:00Z")), {
    since: "2025-07-06",
    until: "2026-08-09",
  });
  assert.throws(() => activity.rangeForDays(30, now), /unsupported/);
});

test("suppressed values never become zero or rates", () => {
  assert.deepEqual(activity.valueState(null), { available: false, value: null });
  assert.deepEqual(activity.valueState(undefined), { available: false, value: null });
  assert.deepEqual(activity.valueState(0), { available: true, value: 0 });
  assert.equal(activity.successRate(null, 10), null);
  assert.equal(activity.successRate(4, null), null);
  assert.equal(activity.successRate(0, 0), null);
  assert.equal(activity.successRate(7, 10), 0.7);
  assert.equal(activity.hasReportableData([{ product_active: null, api_attempts: null }]), false);
  assert.equal(activity.hasReportableData([{ product_active: 0 }]), true);
  assert.equal(activity.hasReportableData([{ game_active: 1 }]), true);
});

test("only the newest range request may update the view", () => {
  const gate = activity.createLatestRequestGate();
  const first = gate.begin();
  const second = gate.begin();
  assert.equal(gate.isCurrent(first), false);
  assert.equal(gate.isCurrent(second), true);
  gate.invalidate();
  assert.equal(gate.isCurrent(second), false);
});

test("Chart registry destroys before replacement and tab return only resizes", () => {
  const events = [];
  const chart = (name) => ({
    destroy() { events.push(`destroy:${name}`); },
    resize() { events.push(`resize:${name}`); },
  });
  const registry = activity.createChartRegistry();
  registry.replace([chart("old")]);
  registry.replace([chart("users"), chart("requests")]);
  registry.resize();
  assert.deepEqual(events, ["destroy:old", "resize:users", "resize:requests"]);
  registry.destroy();
  assert.deepEqual(events.slice(-2), ["destroy:users", "destroy:requests"]);
  assert.equal(registry.size(), 0);
});
