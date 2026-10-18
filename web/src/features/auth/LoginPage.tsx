import { signInURL } from "@/api/auth";
import { APP_NAME } from "@/config";
import { s } from "@/strings";

/** Why a sign-in came back here, as the callback names it, in words. */
function reason(error: string | undefined): string | undefined {
  if (!error) return undefined;
  const known: Record<string, string> = s.signIn.errors;
  return known[error] ?? s.signIn.errors.failed;
}

/** The one way in: the suite's identity provider. A failed attempt lands here with the reason. */
export function LoginPage({ next, error }: { next?: string; error?: string }) {
  const why = reason(error);
  return (
    <main id="main" className="flex min-h-full items-center justify-center bg-backdrop px-4 py-12">
      <section className="w-full max-w-sm rounded-overlay border border-border bg-surface p-6 shadow-2" aria-labelledby="sign-in-title">
        <h1 id="sign-in-title" className="mb-2 text-xl font-semibold text-ink">
          {s.signIn.title(APP_NAME)}
        </h1>
        <p className="mb-5 text-sm text-ink-muted">{s.signIn.body}</p>
        {why && (
          <p role="alert" className="mb-4 rounded-control border border-danger bg-danger-subtle px-3 py-2 text-sm text-danger" data-sign-in-error>
            {why}
          </p>
        )}
        <a
          href={signInURL(next)}
          className="flex h-9 w-full items-center justify-center rounded-control bg-accent px-4 text-sm font-medium text-on-accent hover:bg-accent-hover"
          data-action="sso"
        >
          {s.signIn.action}
        </a>
      </section>
    </main>
  );
}
