import { test as setup } from "@playwright/test";
import { ALICE_STATE, PEOPLE, signIn } from "../fixtures/auth";

// One real sign-in through Keycloak, kept for every spec that only needs somebody signed in.
setup("alice signs in", async ({ page }) => {
  await signIn(page, PEOPLE.alice);
  await page.context().storageState({ path: ALICE_STATE });
});
