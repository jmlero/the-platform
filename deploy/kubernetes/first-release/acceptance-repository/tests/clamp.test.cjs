"use strict";
const { test } = require("node:test");
const assert = require("node:assert/strict");
const { clamp } = require("../src/clamp.cjs");

test("preserves values inside the interval", () => assert.equal(clamp(4, 0, 10), 4));
test("lower bound is inclusive", () => assert.equal(clamp(-5, 0, 10), 0));
test("upper bound is inclusive", () => assert.equal(clamp(15, 0, 10), 10));
test("preserves a value at the upper bound", () => assert.equal(clamp(10, 0, 10), 10));
test("supports negative intervals", () => assert.equal(clamp(0, -10, -2), -2));
test("supports a single-value interval", () => assert.equal(clamp(7, 3, 3), 3));
test("rejects an inverted interval", () => assert.throws(() => clamp(0, 5, 1), RangeError));
