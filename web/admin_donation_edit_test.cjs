"use strict";

const assert = require("assert");
const fs = require("fs");
const vm = require("vm");
const path = require("path");

const adminPath = path.join(__dirname, "static", "admin.js");
const source = fs.readFileSync(adminPath, "utf8");
const startMarker = "// S2_DONATION_EDIT_HELPERS_START";
const endMarker = "// S2_DONATION_EDIT_HELPERS_END";
const start = source.indexOf(startMarker);
const end = source.indexOf(endMarker);
assert(start >= 0 && end > start, "donation edit helpers must remain testable");

const context = {
  fmtLocalDT: () => "2026-08-10T12:00",
};
vm.createContext(context);
vm.runInContext(source.slice(start + startMarker.length, end), context);

const donation = {
  service: "general",
  model: "model-a",
  dify_base_url: "https://dify.example.com/v1",
  deadline: 1,
  total_count: 10,
  rpm_limit: 5,
  note: "donor note",
  review_note: "review note",
  has_review_record: true,
};
const unchanged = {
  service: "general",
  model: "model-a",
  dify_base_url: "https://dify.example.com/v1",
  dify_api_key: "   ",
  deadline: "2026-08-10T12:00",
  total_count: "10",
  rpm_limit: "5",
  note: "donor note",
  review_note: "review note",
};

assert.deepStrictEqual(
  JSON.parse(JSON.stringify(context.buildDonationPatchBody(donation, unchanged))),
  {},
  "unchanged fields and blank API key must be omitted",
);

const serviceOnly = { ...unchanged, service: "agent" };
assert.deepStrictEqual(
  JSON.parse(JSON.stringify(context.buildDonationPatchBody(donation, serviceOnly))),
  { service: "agent" },
  "changing service must preserve omitted notes",
);

const cleared = { ...unchanged, note: "", review_note: "" };
assert.deepStrictEqual(
  JSON.parse(JSON.stringify(context.buildDonationPatchBody(donation, cleared))),
  { note: "", review_note: "" },
  "explicitly cleared notes must be sent",
);

const withoutReview = { ...donation, has_review_record: false };
const attemptedReview = { ...unchanged, review_note: "must not send" };
assert.deepStrictEqual(
  JSON.parse(JSON.stringify(context.buildDonationPatchBody(withoutReview, attemptedReview))),
  {},
  "review_note must never be sent without a review record",
);

const replacementKey = { ...unchanged, dify_api_key: " replacement " };
assert.deepStrictEqual(
  JSON.parse(JSON.stringify(context.buildDonationPatchBody(donation, replacementKey))),
  { dify_api_key: "replacement" },
  "non-blank replacement key must be trimmed and sent",
);

const saveState = { saving: false };
assert.strictEqual(context.beginDonationSave(saveState), true, "first save should start");
assert.strictEqual(context.beginDonationSave(saveState), false, "rapid second save should be ignored");
saveState.saving = false;
assert.strictEqual(context.beginDonationSave(saveState), true, "save should be retryable after a failed request");

const dialogStart = source.indexOf("async function showDonationEditDialog");
const dialogEnd = source.indexOf("/* ---------------- admin bulletin management", dialogStart);
const dialogSource = source.slice(dialogStart, dialogEnd);
assert(dialogSource.includes("saveState.saving = false"), "failed saves must release the submit guard");
assert(dialogSource.includes("saveButton.disabled = false"), "failed saves must re-enable the button");
const catchStart = dialogSource.indexOf("} catch (err) {");
const catchBody = dialogSource.slice(catchStart, dialogSource.indexOf("\n    }", catchStart));
assert(!catchBody.includes("close()"), "400/error responses must leave the form open");

console.log("admin donation edit contract: ok");
