// Irvine local time and the sun over UCI, from a UTC timestamp (visual-style.md,
// "Day and night"; D-050). Pure: no DOM, no three. The sun follows the NOAA solar
// position algorithm (the NOAA Solar Calculator spreadsheet).
//
// Angles are in radians. Elevation is geometric (no refraction), measured up from
// the horizon. Azimuth is measured clockwise from north: 0 = north, π/2 = east,
// π = south, 3π/2 = west, in [0, 2π).

export const IRVINE_TIME_ZONE = 'America/Los_Angeles';
export const UCI_LATITUDE = 33.646;
export const UCI_LONGITUDE = -117.843;

/** Sunrise and sunset: the sun's centre at -0.833° (refraction plus the solar radius). */
export const SUNRISE_ELEVATION = rad(-0.833);
/** Below this the sun is past civil twilight: night. */
export const NIGHT_BELOW = rad(-6);
/** Above this it is full day; between NIGHT_BELOW and here it is dawn or dusk. */
export const DAY_ABOVE = rad(6);

export type DayPhase = 'night' | 'dawn' | 'day' | 'dusk';

export interface IrvineTime {
  year: number;
  /** 1 to 12. */
  month: number;
  day: number;
  hour: number;
  minute: number;
  /** Offset from UTC in minutes: -420 in PDT, -480 in PST. */
  offsetMinutes: number;
}

export interface SunPosition {
  elevation: number;
  azimuth: number;
}

export interface SunTimes {
  sunrise: number;
  sunset: number;
}

const MS_PER_DAY = 86_400_000;
const MS_PER_MIN = 60_000;

function rad(deg: number): number {
  return (deg * Math.PI) / 180;
}

function deg(r: number): number {
  return (r * 180) / Math.PI;
}

const formatter = new Intl.DateTimeFormat('en-US', {
  timeZone: IRVINE_TIME_ZONE,
  hourCycle: 'h23',
  year: 'numeric',
  month: 'numeric',
  day: 'numeric',
  hour: 'numeric',
  minute: 'numeric',
});

/** The wall-clock date and time in Irvine, daylight saving included. */
export function irvineTime(utcMs: number): IrvineTime {
  const parts: Record<string, number> = {};
  for (const p of formatter.formatToParts(new Date(utcMs))) {
    if (p.type !== 'literal') parts[p.type] = Number(p.value);
  }
  const { year, month, day, hour, minute } = parts;
  const asUtc = Date.UTC(year, month - 1, day, hour, minute);
  const offsetMinutes = Math.round((asUtc - Math.floor(utcMs / MS_PER_MIN) * MS_PER_MIN) / MS_PER_MIN);
  return { year, month, day, hour, minute, offsetMinutes };
}

interface SolarTerms {
  /** Sun's declination, radians. */
  declination: number;
  /** Equation of time, minutes. */
  eqTime: number;
}

// NOAA: Julian century from the UTC timestamp, then declination and equation of time.
function solarTerms(utcMs: number): SolarTerms {
  const jd = utcMs / MS_PER_DAY + 2440587.5;
  const t = (jd - 2451545) / 36525;
  const l0 = (280.46646 + t * (36000.76983 + t * 0.0003032)) % 360;
  const m = 357.52911 + t * (35999.05029 - 0.0001537 * t);
  const e = 0.016708634 - t * (0.000042037 + 0.0000001267 * t);
  const mr = rad(m);
  const c =
    Math.sin(mr) * (1.914602 - t * (0.004817 + 0.000014 * t)) +
    Math.sin(2 * mr) * (0.019993 - 0.000101 * t) +
    Math.sin(3 * mr) * 0.000289;
  const trueLong = l0 + c;
  const omega = rad(125.04 - 1934.136 * t);
  const appLong = trueLong - 0.00569 - 0.00478 * Math.sin(omega);
  const meanObliq = 23 + (26 + (21.448 - t * (46.815 + t * (0.00059 - t * 0.001813))) / 60) / 60;
  const obliq = rad(meanObliq + 0.00256 * Math.cos(omega));
  const declination = Math.asin(Math.sin(obliq) * Math.sin(rad(appLong)));
  const y = Math.tan(obliq / 2) ** 2;
  const l0r = rad(l0);
  const eqTime =
    4 *
    deg(
      y * Math.sin(2 * l0r) -
        2 * e * Math.sin(mr) +
        4 * e * y * Math.sin(mr) * Math.cos(2 * l0r) -
        0.5 * y * y * Math.sin(4 * l0r) -
        1.25 * e * e * Math.sin(2 * mr),
    );
  return { declination, eqTime };
}

/** The sun's elevation and azimuth (radians, azimuth clockwise from north) at UCI. */
export function sunPosition(utcMs: number): SunPosition {
  const { declination, eqTime } = solarTerms(utcMs);
  const utcMinutes = (((utcMs % MS_PER_DAY) + MS_PER_DAY) % MS_PER_DAY) / MS_PER_MIN;
  const trueSolarMinutes = (((utcMinutes + eqTime + 4 * UCI_LONGITUDE) % 1440) + 1440) % 1440;
  const hourAngle = rad(trueSolarMinutes / 4 - 180);
  const lat = rad(UCI_LATITUDE);
  const cosZenith =
    Math.sin(lat) * Math.sin(declination) + Math.cos(lat) * Math.cos(declination) * Math.cos(hourAngle);
  const zenith = Math.acos(Math.min(1, Math.max(-1, cosZenith)));
  // Azimuth from north, clockwise, via atan2 so it is defined at every hour angle.
  const azimuth = Math.atan2(
    Math.sin(hourAngle),
    Math.cos(hourAngle) * Math.sin(lat) - Math.tan(declination) * Math.cos(lat),
  ) + Math.PI;
  return { elevation: Math.PI / 2 - zenith, azimuth: azimuth % (2 * Math.PI) };
}

// Minutes after UTC midnight of baseMs at which the sun crosses SUNRISE_ELEVATION,
// rising (sign -1) or setting (sign +1). Refined by recomputing the terms at the estimate.
function crossing(baseMs: number, sign: -1 | 1): number {
  let minutes = 720 - 4 * UCI_LONGITUDE;
  const lat = rad(UCI_LATITUDE);
  for (let i = 0; i < 3; i++) {
    const { declination, eqTime } = solarTerms(baseMs + minutes * MS_PER_MIN);
    const cosH =
      (Math.sin(SUNRISE_ELEVATION) - Math.sin(lat) * Math.sin(declination)) /
      (Math.cos(lat) * Math.cos(declination));
    const ha = deg(Math.acos(Math.min(1, Math.max(-1, cosH))));
    minutes = 720 - 4 * (UCI_LONGITUDE - sign * ha) - eqTime;
  }
  return minutes;
}

/** Sunrise and sunset (UTC ms) for the Irvine calendar date that contains utcMs. */
export function sunTimes(utcMs: number): SunTimes {
  const { year, month, day } = irvineTime(utcMs);
  const base = Date.UTC(year, month - 1, day);
  return {
    sunrise: Math.round(base + crossing(base, -1) * MS_PER_MIN),
    sunset: Math.round(base + crossing(base, 1) * MS_PER_MIN),
  };
}

/** night below -6°, day above +6°, and dawn or dusk between, by whether the sun is rising. */
export function dayPhase(utcMs: number): DayPhase {
  const { elevation } = sunPosition(utcMs);
  if (elevation < NIGHT_BELOW) return 'night';
  if (elevation > DAY_ABOVE) return 'day';
  return sunPosition(utcMs + MS_PER_MIN).elevation > elevation ? 'dawn' : 'dusk';
}
