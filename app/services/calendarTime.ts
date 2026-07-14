export type LocalDateTimeResolution =
  | { status: "invalid"; message: string }
  | { status: "gap"; message: string }
  | { status: "ambiguous"; message: string; instants: [string, string] }
  | { status: "resolved"; instant: string };

type DateParts = {
  year: number;
  month: number;
  day: number;
  hour: number;
  minute: number;
};

const datePattern = /^(\d{4})-(\d{2})-(\d{2})$/;
const timePattern = /^(\d{1,2}):(\d{2})$/;

function formatter(timeZone: string) {
  return new Intl.DateTimeFormat("en-US-u-ca-gregory-nu-latn", {
    timeZone,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hourCycle: "h23",
  });
}

function partsAt(value: Date, timeZone: string): DateParts {
  const values = Object.fromEntries(
    formatter(timeZone)
      .formatToParts(value)
      .filter((part) => part.type !== "literal")
      .map((part) => [part.type, Number(part.value)]),
  );
  return {
    year: values.year,
    month: values.month,
    day: values.day,
    hour: values.hour,
    minute: values.minute,
  };
}

function sameParts(left: DateParts, right: DateParts) {
  return (
    left.year === right.year &&
    left.month === right.month &&
    left.day === right.day &&
    left.hour === right.hour &&
    left.minute === right.minute
  );
}

function parseLocal(date: string, time: string): DateParts | null {
  const dateMatch = datePattern.exec(date.trim());
  const timeMatch = timePattern.exec(time.trim());
  if (!dateMatch || !timeMatch) return null;
  const parts = {
    year: Number(dateMatch[1]),
    month: Number(dateMatch[2]),
    day: Number(dateMatch[3]),
    hour: Number(timeMatch[1]),
    minute: Number(timeMatch[2]),
  };
  const check = new Date(
    Date.UTC(parts.year, parts.month - 1, parts.day, parts.hour, parts.minute),
  );
  if (
    parts.year < 1 ||
    parts.hour > 23 ||
    parts.minute > 59 ||
    check.getUTCFullYear() !== parts.year ||
    check.getUTCMonth() + 1 !== parts.month ||
    check.getUTCDate() !== parts.day
  ) {
    return null;
  }
  return parts;
}

/**
 * Resolves a wall-clock date and time without relying on the device timezone.
 * DST gaps are rejected and repeated wall times return both exact instants so
 * the caller must record an explicit choice.
 */
export function resolveLocalDateTime(
  date: string,
  time: string,
  timeZone: string,
): LocalDateTimeResolution {
  const requested = parseLocal(date, time);
  if (!requested) {
    return {
      status: "invalid",
      message: "Enter a valid date (YYYY-MM-DD) and time (HH:MM).",
    };
  }
  const zone = timeZone.trim();
  try {
    formatter(zone).format(new Date(0));
  } catch {
    return {
      status: "invalid",
      message: "Enter a valid IANA timezone, such as Asia/Shanghai.",
    };
  }

  const wallClockUTC = Date.UTC(
    requested.year,
    requested.month - 1,
    requested.day,
    requested.hour,
    requested.minute,
  );
  const offsets = new Set<number>();
  for (let hours = -36; hours <= 36; hours += 6) {
    const sample = new Date(wallClockUTC + hours * 60 * 60 * 1000);
    const local = partsAt(sample, zone);
    const localAsUTC = Date.UTC(
      local.year,
      local.month - 1,
      local.day,
      local.hour,
      local.minute,
    );
    offsets.add(localAsUTC - sample.getTime());
  }
  const matches = [...offsets]
    .map((offset) => new Date(wallClockUTC - offset))
    .filter((candidate) => sameParts(partsAt(candidate, zone), requested))
    .map((candidate) => candidate.toISOString())
    .sort()
    .filter((value, index, all) => index === 0 || value !== all[index - 1]);

  if (matches.length === 0) {
    return {
      status: "gap",
      message: `${date} at ${time} does not exist in ${zone} because the clock moves forward. Choose another time.`,
    };
  }
  if (matches.length > 1) {
    return {
      status: "ambiguous",
      message: `${date} at ${time} occurs twice in ${zone} because the clock moves back. Choose the first or second occurrence.`,
      instants: [matches[0], matches[matches.length - 1]],
    };
  }
  return { status: "resolved", instant: matches[0] };
}

export function localFieldsFromInstant(instant: string, timeZone: string) {
  const parts = partsAt(new Date(instant), timeZone);
  const pad = (value: number) => String(value).padStart(2, "0");
  return {
    date: `${String(parts.year).padStart(4, "0")}-${pad(parts.month)}-${pad(parts.day)}`,
    time: `${pad(parts.hour)}:${pad(parts.minute)}`,
  };
}

export function formatResolvedInstant(
  instant: string,
  timeZone: string,
  locale?: string,
) {
  return new Intl.DateTimeFormat(locale, {
    weekday: "long",
    year: "numeric",
    month: "long",
    day: "numeric",
    hour: "numeric",
    minute: "2-digit",
    timeZone,
    timeZoneName: "short",
  }).format(new Date(instant));
}
