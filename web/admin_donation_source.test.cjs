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
const optionStart = adminSource.indexOf("function adminUserCanonicalValue");
const optionEnd = adminSource.indexOf("\nasync function initAdminUsersTab", optionStart);
assert(optionStart >= 0 && optionEnd > optionStart, "datalist option helpers must remain testable");
const submitStart = adminSource.indexOf("async function onDonationSubmit");
const submitEnd = adminSource.indexOf("/* ---------------- admin donation review", submitStart);
assert(submitStart >= 0 && submitEnd > submitStart, "donation submit handler must remain testable");
const exportStart = adminSource.indexOf("async function onExportLogs");
const exportEnd = adminSource.indexOf("\nlet _adminLogStatsRequest", exportStart);
assert(exportStart >= 0 && exportEnd > exportStart, "log export handler must remain testable");
const filterStart = adminSource.indexOf("function applyDonationFilters");
const filterEnd = adminSource.indexOf("\nasync function loadPricing", filterStart);
assert(filterStart >= 0 && filterEnd > filterStart, "donation filter must remain testable");

const sourceInput = { value: "" };
const donationNote = { innerHTML: "" };
const filterElements = {
  "#alf-user": { value: "" },
  "#alf-service": { value: "" },
  "#alf-model": { value: "" },
  "#alf-status": { value: "" },
  "#alf-since": { value: "" },
  "#alf-until": { value: "" },
  "#don-filter-status": { value: "" },
  "#don-filter-service": { value: "" },
  "#don-filter-q": { value: "" },
  "#don-filter-user": { value: "" },
};
const toastMessages = [];
let submittedBody;
const context = {
  adminLogUsers: [],
  T: (key) => key === "adminLogsUserAmbiguous" ? "ambiguous: {name}" : key,
  esc: (value) => String(value ?? "").replace(/[&<>"']/g, (c) => ({
    "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;",
  }[c])),
  coAdminPath: (value) => value,
  loadAdminDonations: async () => {},
  $(selector) {
    if (selector === "#don-source-user") return sourceInput;
    if (selector === "#don-note") return donationNote;
    if (filterElements[selector]) return filterElements[selector];
    throw new Error(`unexpected selector: ${selector}`);
  },
  api: async (_url, options) => {
    submittedBody = options.body;
    return {};
  },
  toast: (message) => toastMessages.push(message),
  confirm: () => false,
  fetch: async () => { throw new Error("fetch must not run for invalid user input"); },
  renderPaged: () => {},
  URLSearchParams,
  donPager: { data: [], page: 1 },
  _allDonations: [],
};
vm.createContext(context);
vm.runInContext(adminSource.slice(optionStart, optionEnd), context);
vm.runInContext(adminSource.slice(resolverStart, resolverEnd), context);
vm.runInContext(adminSource.slice(submitStart, submitEnd), context);
vm.runInContext(adminSource.slice(exportStart, exportEnd), context);
vm.runInContext(adminSource.slice(filterStart, filterEnd), context);

function setUsers(users) {
  context.adminLogUsers = users;
}

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
  donationNote.innerHTML = "";
  submittedBody = undefined;
  await context.onDonationSubmit({
    preventDefault() {},
    target: makeForm(sourceText),
  });
  return submittedBody;
}

test("donation source picker resolves all four unambiguous formats", async () => {
  setUsers([
    { id: 42, username: "Alice", discord_id: "alice-42" },
    { id: 73, username: "Bob", discord_id: "bob-73" },
  ]);
  const cases = [
    ["Alice", 42],
    ["alice-42", 42],
    ["42", 42],
    ["Alice（alice-42） [42]", 42],
    ["Bob（bob-73）", 73],
  ];
  for (const [userText, expectedID] of cases) {
    const body = await submitDonation(userText);
    assert.equal(body.source_user_id, expectedID, `source user should resolve for ${userText}`);
    assert.equal(body.source_text, "", `resolved user should not create fallback text for ${userText}`);
  }
});

test("colliding username, Discord id, and duplicate username are ambiguous", () => {
  setUsers([
    { id: 42, username: "target", discord_id: "victim-discord" },
    { id: 7, username: "42", discord_id: "attacker-discord" },
  ]);
  assert.equal(context.resolveLogUserFilter("42").ambiguous, true, "numeric username must not hide DB id");

  setUsers([
    { id: 1, username: "same", discord_id: "discord-one" },
    { id: 2, username: "other", discord_id: "same" },
  ]);
  assert.equal(context.resolveLogUserFilter("same").ambiguous, true, "username/Discord collision must be rejected");

  setUsers([
    { id: 1, username: "duplicate", discord_id: "discord-one" },
    { id: 2, username: "duplicate", discord_id: "discord-two" },
  ]);
  assert.equal(context.resolveLogUserFilter("duplicate").ambiguous, true, "duplicate usernames must be rejected");

  const shaped = "Alice（alice-discord） [42]";
  setUsers([
    { id: 42, username: "Alice", discord_id: "alice-discord" },
    { id: 7, username: shaped, discord_id: "shaped-discord" },
  ]);
  assert.equal(context.resolveLogUserFilter(shaped).ambiguous, true, "composite-shaped username must not hide composite id");
});

test("inconsistent composite fields are rejected and do not submit", async () => {
  setUsers([
    { id: 1, username: "Alice", discord_id: "alice-discord" },
    { id: 2, username: "Bob", discord_id: "bob-discord" },
  ]);
  const mismatched = context.resolveLogUserFilter("Alice（bob-discord） [1]");
  assert.equal(mismatched.ambiguous, true);
  assert.match(mismatched.error, /ambiguous/i);

  const body = await submitDonation("Alice（bob-discord） [1]");
  assert.equal(body, undefined, "ambiguous source must not call the donation API");
  assert.match(donationNote.innerHTML, /ambiguous/i, "admin must see an understandable ambiguity error");
});

test("canonical datalist value resolves after selection and escapes special characters", async () => {
  const user = { id: 9, username: "A\"<&", discord_id: "D'>&" };
  setUsers([user]);
  const canonical = context.adminUserCanonicalValue(user);
  assert.equal(canonical, "A\"<&（D'>&） [9]");
  assert.equal(
    context.adminUserOption(user),
    '<option value="A&quot;&lt;&amp;（D&#39;&gt;&amp;） [9]"></option>',
  );
  const body = await submitDonation(canonical);
  assert.equal(body.source_user_id, 9, "selected canonical option must submit the DB id");
  assert.equal(body.source_text, "");
});

test("unmatched source input is preserved and explicit source text wins", async () => {
  setUsers([{ id: 42, username: "Alice", discord_id: "alice-42" }]);
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

test("invalid resolver results cannot export or broaden donation filters", async () => {
  setUsers([
    { id: 42, username: "target", discord_id: "victim-discord" },
    { id: 7, username: "42", discord_id: "attacker-discord" },
  ]);
  filterElements["#alf-user"].value = "42";
  toastMessages.length = 0;
  await context.onExportLogs();
  assert.equal(toastMessages.length, 1, "ambiguous export input should show one error toast");
  assert.match(toastMessages[0], /ambiguous/i);

  context._allDonations = [
    { id: 1, source_user_id: 42, status: "active", service: "general", model: "a" },
  ];
  filterElements["#don-filter-user"].value = "42";
  context.applyDonationFilters();
  assert.equal(context.donPager.data.length, 0, "ambiguous donation filter must not fall back to all rows");

  filterElements["#don-filter-user"].value = "not-registered";
  context.applyDonationFilters();
  assert.equal(context.donPager.data.length, 0, "unmatched donation filter must not fall back to all rows");
});

console.log("admin donation source contract: ok");
