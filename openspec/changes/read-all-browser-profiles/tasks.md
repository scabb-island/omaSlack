# Tasks — read-all-browser-profiles

- [x] 1.1 `auth`: `FindBrowsers` enumerates every profile (Default, Profile N) of each browser that has a Local Storage, skipping System/Guest profiles
- [x] 1.2 `auth`: profile display name from the browser's `Local State` (profile.info_cache); non-Default profiles are named "<Browser> (<name>)", Default stays "<Browser>"
- [x] 1.3 `auth`: tests (several profiles, display names, skipped dirs, dedup across config homes)
- [x] 2.1 Live: A3Mercados in the non-Default Brave profile is found read-only and auth.test accepts it (no store)
- [x] 2.2 `make local` green; MR
