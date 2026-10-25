// Shared setup of every test file (vite.config.ts, test.setupFiles).
//
// Preact 11 runs the passive effect cleanups of an unmounted component after
// the next frame (at most 35 ms later). If a test file ends first, such a
// cleanup runs after jsdom is gone and fails with "window is not defined".
// Vitest runs this hook after the afterEach hooks of the test files, so every
// test unmounts its components and then waits for those cleanups here.
import { cleanup, configure } from "@testing-library/preact";
import { afterEach } from "vitest";

// A loaded CI runner can need more than the default 1 s for findBy and waitFor.
configure({ asyncUtilTimeout: 3000 });

afterEach(async () => {
  cleanup();
  await new Promise((r) => setTimeout(r, 60));
});
