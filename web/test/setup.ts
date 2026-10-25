// Shared setup of every test file (vite.config.ts, test.setupFiles).
//
// Preact 11 runs the passive effect cleanups of an unmounted component after
// the next frame (at most 35 ms later). If a test file ends first, such a
// cleanup runs after jsdom is gone and fails with "window is not defined".
// Vitest runs this hook after the afterEach hooks of the test files, so every
// test unmounts its components and then waits for those cleanups here.
import { cleanup, configure, within } from "@testing-library/preact";
import { afterEach, beforeAll } from "vitest";

// A loaded CI runner can need more than the default 1 s for findBy and waitFor.
configure({ asyncUtilTimeout: 3000 });

// Each test file gets a new jsdom. jsdom parses its default stylesheet on the
// first getComputedStyle call, and Testing Library builds its role tables on
// the first *ByRole query. That one-time work is about 0.5 s of CPU, and many
// seconds on a loaded host. Do it here, before the tests, so that it does not
// count against the time limits of the first test that queries by role.
beforeAll(() => {
  const b = document.createElement("button");
  b.textContent = "warm-up";
  document.body.appendChild(b);
  within(document.body).getByRole("button", { name: "warm-up" });
  b.remove();
});

afterEach(async () => {
  cleanup();
  await new Promise((r) => setTimeout(r, 60));
});
