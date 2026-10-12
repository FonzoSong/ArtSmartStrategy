import assert from "node:assert/strict";
import test from "node:test";

import {
  MISSING,
  formatCount,
  formatDecimal,
  formatPercent,
  formatPercentagePoints,
  formatRatioPercent,
  formatScore,
  isFiniteNumber,
  toFiniteNumber,
} from "../src/lib/format.ts";

test("formatPercent keeps two decimals for percentage-valued metrics", () => {
  assert.equal(formatPercent(0), "0.00%");
  assert.equal(formatPercent(1.2), "1.20%");
  assert.equal(formatPercent(1.234), "1.23%");
  assert.equal(formatPercent(12.5), "12.50%");
  assert.equal(formatPercent(100), "100.00%");
  assert.equal(formatPercent(-3.456), "-3.46%");
  assert.equal(formatPercent("12.5"), "12.50%");
});

test("formatPercent shows a missing state instead of guessing", () => {
  assert.equal(formatPercent(null), MISSING);
  assert.equal(formatPercent(undefined), MISSING);
  assert.equal(formatPercent(""), MISSING);
  assert.equal(formatPercent("不是数字"), MISSING);
  assert.equal(formatPercent(Number.NaN), MISSING);
  assert.equal(formatPercent(Number.POSITIVE_INFINITY), MISSING);
  assert.equal(formatPercent({}), MISSING);
});

test("formatRatioPercent scales ratio-valued metrics", () => {
  assert.equal(formatRatioPercent(0.125), "12.50%");
  assert.equal(formatRatioPercent(0), "0.00%");
  assert.equal(formatRatioPercent(1), "100.00%");
  assert.equal(formatRatioPercent(null), MISSING);
});

test("formatPercentagePoints keeps the value without the percent sign", () => {
  assert.equal(formatPercentagePoints(2.5), "2.50");
  assert.equal(formatPercentagePoints(-0.005), "-0.01");
  assert.equal(formatPercentagePoints("abc"), MISSING);
});

test("formatCount never renders a percentage", () => {
  assert.equal(formatCount(0), "0");
  assert.equal(formatCount(12345), "12,345");
  assert.equal(formatCount(null), "0");
  assert.equal(formatCount("abc"), "0");
});

test("formatDecimal and formatScore handle boundaries", () => {
  assert.equal(formatDecimal(1.234), "1.23");
  assert.equal(formatDecimal(null), MISSING);
  assert.equal(formatScore(87.25), "87.3分");
  assert.equal(formatScore(null), "样本不足");
});

test("numeric helpers separate finite numbers from everything else", () => {
  assert.equal(isFiniteNumber(1), true);
  assert.equal(isFiniteNumber("1"), false);
  assert.equal(isFiniteNumber(Number.NaN), false);
  assert.equal(toFiniteNumber(" 1.5 "), 1.5);
  assert.equal(toFiniteNumber(""), null);
  assert.equal(toFiniteNumber(true), null);
  assert.equal(toFiniteNumber([1]), null);
});
