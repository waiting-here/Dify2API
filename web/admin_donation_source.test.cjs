"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

const adminSource = fs.readFileSync(path.join(__dirname, "static", "admin.js"), "utf8");
const resolverStart = adminSource.indexOf("function resolveLogUserFilter");
const resolverEnd = adminSource.indexOf("\nfunction closeDropdownMenus", resolverStart);
assert(resolverStart >= 0 && resolverEnd > resolverStart, "donation resolver must remain testable");

const submitStart = adminSource.indexOf("async function onDonationSubmit");
const submitEnd = adminSource.indexOf("/* ---------------- admin donation review", submitStart);
assert(submitStart >= 0 && submitEnd > submitStart, "donation submit handler must remain testable");

const sourceUsers = [
  { id: 42, username: "Alice", discord_id: "alice-42" },
  { id: 73, username: "Bob", discord_id: "bob-73" },
];
const sourceInput = { value: "" };
const donationNote = { innerHTML: "" };
let submittedBody;

const context = {
  adminLogUsers: sourceUsers,
  T: (key) => key,
  esc: (value) => String(value),
  coAdminPath: (value) => value,
  loadAdminDonations: async () => {},
  $(selector) {
    if (selector === "#don-source-user") return sourceInput;
    if (selector === "#don-note") return donationNote;
    throw new Error(`unexpected selector: ${selector}`);
  },
  api: async (_url, options) => {
    submittedBody = options.body;
    return {};
  },
};
vm.createContext(context);
vm.runInContext(adminSource.slice(resolverStart, resolverEnd), context);
vm.runInContext(adminSource.slice(submitStart, submitEnd), context);

function makeForm(sourceText) {
  return {
    service: { value: "general" },
    model: { value: "model-a" },
    dify_base_url: { value: "https://dify.example/v1" },
    dify_api_key: { value: "app-key" },
    source_text: { value: sourceText },
    deadline: { value: "2026-08-16T12:00" },
    total_count: { value: "10" },
    rpm_limit: { value: "5" },
    note: { value: "note" },
    reset() {},
  };
}

async function submitDonation(userText, sourceText = "") {
  sourceInput.value = userText;
  submittedBody = undefined;
  await context.onDonationSubmit({
    preventDefault() {},
    target: makeForm(sourceText),
  });
  return submittedBody;
}

test("donation source picker resolves all supported formats", async () => {
  const cases = [
    ["Alice", 42],
    ["alice-42", 42],
    ["42", 42],
    ["Alice（alice-42） [42]", 42],
  ];
  for (const [userText, expectedID] of cases) {
    const body = await submitDonation(userText);
    assert.equal(body.source_user_id, expectedID, `source user should resolve for ${userText}`);
    assert.equal(body.source_text, "", `resolved user should not create fallback text for ${userText}`);
  }
});

test("unmatched source input is preserved and explicit source text wins", async () => {
  const fallback = await submitDonation("unknown donor");
  assert.equal(fallback.source_user_id, null);
  assert.equal(fallback.source_text, "unknown donor");

  const explicit = await submitDonation("unknown donor", "campaign landing page");
  assert.equal(explicit.source_user_id, null);
  assert.equal(explicit.source_text, "campaign landing page");

  const matchedWithExplicit = await submitDonation("Alice", "manual attribution");
  assert.equal(matchedWithExplicit.source_user_id, 42);
  assert.equal(matchedWithExplicit.source_text, "manual attribution");
});

console.log("admin donation source contract: ok");
