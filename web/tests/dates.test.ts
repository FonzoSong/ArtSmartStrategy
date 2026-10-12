import assert from "node:assert/strict";
import test from "node:test";

import { dateTimeText, isDateKey, localDateKey, monthGridDates, monthRange, pureDateKey, weekdayLabelOf, withinWindow } from "../src/lib/dates.ts";

test("pureDateKey keeps the calendar date of timestamps", () => {
  assert.equal(pureDateKey("2026-10-12"), "2026-10-12");
  assert.equal(pureDateKey("2026-10-12T00:00:00Z"), "2026-10-12");
  assert.equal(pureDateKey(" 2026-03-01T23:59:59+08:00 "), "2026-03-01");
  assert.equal(pureDateKey("2026-10-12 19:30:00"), "2026-10-12");
});

test("pureDateKey rejects values that are not calendar dates", () => {
  assert.equal(pureDateKey(null), "");
  assert.equal(pureDateKey(undefined), "");
  assert.equal(pureDateKey(""), "");
  assert.equal(pureDateKey("12/10/2026"), "");
  assert.equal(pureDateKey(20261012), "");
});

test("isDateKey validates real calendar dates", () => {
  assert.equal(isDateKey("2026-10-12"), true);
  assert.equal(isDateKey("2028-02-29"), true);
  assert.equal(isDateKey("2027-02-29"), false);
  assert.equal(isDateKey("2026-13-01"), false);
  assert.equal(isDateKey("2026-10-12T00:00:00Z"), false);
});

test("calendar day keys match the month grid cells", () => {
  const anchor = new Date(2026, 9, 1); // October 2026 starts on a Thursday
  const grid = monthGridDates(anchor);
  assert.equal(grid.length, 42);
  assert.equal(localDateKey(grid[0]), "2026-09-28");
  assert.equal(localDateKey(grid[41]), "2026-11-08");
  assert.equal(grid.filter((day) => day.getMonth() === 9).length, 31);
  assert.ok(grid.some((day) => localDateKey(day) === "2026-10-12"));
});

test("monthRange covers month start, month end and leap days", () => {
  assert.deepEqual(monthRange(new Date(2026, 9, 15)), { first: "2026-10-01", last: "2026-10-31" });
  assert.deepEqual(monthRange(new Date(2026, 1, 10)), { first: "2026-02-01", last: "2026-02-28" });
  assert.deepEqual(monthRange(new Date(2028, 1, 10)), { first: "2028-02-01", last: "2028-02-29" });
});

test("month grid normalizes API dates before matching a cell", () => {
  const grid = monthGridDates(new Date(2026, 9, 1));
  const key = localDateKey(grid.find((day) => localDateKey(day) === "2026-10-12")!);
  const apiDates = ["2026-10-12T00:00:00Z", "2026-10-11T16:00:00Z"];
  const matched = apiDates.filter((value) => pureDateKey(value) === key);
  assert.deepEqual(matched, ["2026-10-12T00:00:00Z"]);
});

test("withinWindow is inclusive on both edges and rejects invalid keys", () => {
  const start = "2026-10-12";
  const end = "2026-10-25";
  assert.equal(withinWindow("2026-10-11", start, end), false);
  assert.equal(withinWindow("2026-10-12", start, end), true);
  assert.equal(withinWindow("2026-10-25", start, end), true);
  assert.equal(withinWindow("2026-10-26", start, end), false);
  assert.equal(withinWindow("", start, end), false);
  assert.equal(withinWindow("2026-10-12T00:00:00Z", start, end), false);
  assert.equal(withinWindow("2026-10-12", null, null), true);
});

test("dateTimeText renders timestamps and missing values", () => {
  assert.equal(dateTimeText("2026-10-12T19:30:00Z"), "2026-10-12 19:30");
  assert.equal(dateTimeText("2026-10-12T19:30:00Z", 10), "2026-10-12");
  assert.equal(dateTimeText(null), "—");
  assert.equal(dateTimeText(undefined), "—");
  assert.equal(dateTimeText(""), "—");
});

test("weekdayLabelOf matches the rule-set weekday labels", () => {
  assert.equal(weekdayLabelOf("2026-10-12"), "周一");
  assert.equal(weekdayLabelOf("2026-10-18"), "周日");
  assert.equal(weekdayLabelOf("2028-02-29"), "周二");
  assert.equal(weekdayLabelOf("2026-10-12T00:00:00Z"), "");
  assert.equal(weekdayLabelOf(""), "");
});
