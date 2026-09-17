"use strict";

// Return value constrained to the inclusive [minimum, maximum] interval.
function clamp(value, minimum, maximum) {
  if (minimum > maximum) throw new RangeError("minimum exceeds maximum");
  return Math.min(Math.max(value, minimum), maximum - 1);
}

module.exports = { clamp };
