/* Dify2API admin activity helpers. Kept DOM-free so range, suppression,
 * request-ordering, and Chart lifecycle contracts can be unit tested. */
"use strict";

(function exposeActivityHelpers(root, factory) {
  const helpers = factory();
  if (typeof module === "object" && module.exports) module.exports = helpers;
  else root.D2AActivity = helpers;
})(typeof globalThis !== "undefined" ? globalThis : this, function activityHelpers() {
  const RANGE_DAYS = Object.freeze([7, 28, 90, 400]);

  function utcDateString(date) {
    return date.toISOString().slice(0, 10);
  }

  function rangeForDays(days, now = new Date()) {
    if (!RANGE_DAYS.includes(days)) throw new RangeError("unsupported activity range");
    const until = new Date(now);
    if (Number.isNaN(until.getTime())) throw new RangeError("invalid activity date");
    const since = new Date(Date.UTC(until.getUTCFullYear(), until.getUTCMonth(), until.getUTCDate()));
    since.setUTCDate(since.getUTCDate() - days + 1);
    return { since: utcDateString(since), until: utcDateString(until) };
  }

  function valueState(value) {
    return typeof value === "number" && Number.isFinite(value)
      ? { available: true, value }
      : { available: false, value: null };
  }

  function successRate(successes, attempts) {
    const success = valueState(successes);
    const total = valueState(attempts);
    if (!success.available || !total.available || total.value <= 0) return null;
    return success.value / total.value;
  }

  function hasReportableData(rows) {
    const fields = [
      "new_users", "product_active", "successful_api_active", "attempted_api_active",
      "console_active", "api_attempts", "api_successes",
    ];
    return Array.isArray(rows) && rows.some((row) => fields.some((field) => valueState(row?.[field]).available));
  }

  function createLatestRequestGate() {
    let generation = 0;
    return Object.freeze({
      begin() { generation += 1; return generation; },
      invalidate() { generation += 1; },
      isCurrent(token) { return token === generation; },
    });
  }

  function createChartRegistry() {
    let charts = [];
    return Object.freeze({
      replace(nextCharts) {
        this.destroy();
        charts = (nextCharts || []).filter(Boolean);
      },
      destroy() {
        for (const chart of charts) {
          try { chart.destroy(); } catch (_) { /* already detached */ }
        }
        charts = [];
      },
      resize() {
        for (const chart of charts) chart.resize();
      },
      size() { return charts.length; },
    });
  }

  return {
    RANGE_DAYS,
    rangeForDays,
    valueState,
    successRate,
    hasReportableData,
    createLatestRequestGate,
    createChartRegistry,
  };
});
