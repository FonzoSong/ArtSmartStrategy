/**
 * Pure calendar-date helpers.
 *
 * Planning dates are calendar dates, not instants. Everything here works on the
 * YYYY-MM-DD text form and never applies a timezone conversion, so a DATE value
 * cannot shift to another day.
 */

export const DATE_KEY_PATTERN = /^\d{4}-\d{2}-\d{2}$/;

/**
 * Normalizes an API date value to a YYYY-MM-DD key. Values such as
 * `2026-10-12T00:00:00Z` keep their calendar date; anything malformed yields an
 * empty string so callers can treat it as "no date" instead of matching wrongly.
 */
export function pureDateKey(value: unknown): string {
  if (typeof value !== "string") {
    return "";
  }
  const trimmed = value.trim();
  if (trimmed.length < 10) {
    return "";
  }
  const candidate = trimmed.slice(0, 10);
  return DATE_KEY_PATTERN.test(candidate) ? candidate : "";
}

/** Reports whether a value is a well-formed YYYY-MM-DD date key. */
export function isDateKey(value: unknown): value is string {
  if (typeof value !== "string" || !DATE_KEY_PATTERN.test(value)) {
    return false;
  }
  const [year, month, day] = value.split("-").map(Number);
  const parsed = new Date(Date.UTC(year, month - 1, day));
  return parsed.getUTCFullYear() === year && parsed.getUTCMonth() === month - 1 && parsed.getUTCDate() === day;
}

/**
 * Renders a Date as YYYY-MM-DD using the local calendar. This is only used for
 * values already constructed locally (the month grid), never for API data.
 */
export function localDateKey(date: Date): string {
  const month = String(date.getMonth() + 1).padStart(2, "0");
  const day = String(date.getDate()).padStart(2, "0");
  return `${date.getFullYear()}-${month}-${day}`;
}

/** Returns the first day of the month containing the anchor date. */
export function monthAnchor(reference: Date, monthOffset = 0): Date {
  return new Date(reference.getFullYear(), reference.getMonth() + monthOffset, 1);
}

/**
 * Returns the inclusive first and last calendar date of the month containing the
 * anchor, using local calendar arithmetic (month end is derived from day 0 of
 * the next month, which keeps leap days correct).
 */
export function monthRange(anchor: Date): { first: string; last: string } {
  return {
    first: localDateKey(new Date(anchor.getFullYear(), anchor.getMonth(), 1)),
    last: localDateKey(new Date(anchor.getFullYear(), anchor.getMonth() + 1, 0)),
  };
}

/**
 * Returns a 6×7 grid of days (Monday first) covering the month of the anchor,
 * including the leading and trailing days of the neighbouring months.
 */
export function monthGridDates(anchor: Date): Date[] {
  const first = new Date(anchor.getFullYear(), anchor.getMonth(), 1);
  const offsetFromMonday = (first.getDay() + 6) % 7;
  const start = new Date(first);
  start.setDate(first.getDate() - offsetFromMonday);
  return Array.from({ length: 42 }, (_, index) => {
    const day = new Date(start);
    day.setDate(start.getDate() + index);
    return day;
  });
}

/** Reports whether a date key lies inside an inclusive window. */
export function withinWindow(dateKey: string, windowStart?: string | null, windowEnd?: string | null): boolean {
  if (!isDateKey(dateKey)) {
    return false;
  }
  const start = pureDateKey(windowStart);
  const end = pureDateKey(windowEnd);
  if (start && dateKey < start) {
    return false;
  }
  if (end && dateKey > end) {
    return false;
  }
  return true;
}

const WEEKDAY_LABELS = ["周日", "周一", "周二", "周三", "周四", "周五", "周六"];

/**
 * Returns the rule-set weekday label for a date key. The Date is built from the
 * calendar parts in local time, so the label never shifts across timezones.
 */
export function weekdayLabelOf(dateKey: string): string {
  if (!isDateKey(dateKey)) {
    return "";
  }
  const [year, month, day] = dateKey.split("-").map(Number);
  return WEEKDAY_LABELS[new Date(year, month - 1, day).getDay()];
}

/** Renders a timestamp for display, tolerating empty values. */
export function dateTimeText(value: unknown, length = 16): string {
  if (typeof value !== "string") {
    return "—";
  }
  const trimmed = value.trim();
  if (!trimmed) {
    return "—";
  }
  return trimmed.slice(0, length).replace("T", " ");
}
