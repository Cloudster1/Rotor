import { useQuery } from "@tanstack/react-query";
import { meQuery } from "@/api/auth";
import { type Limits, myUsageQuery, peopleUsageQuery, type UsageDay } from "@/api/usage";
import { PageHeader } from "@/features/shell/EmptyState";
import { s } from "@/strings";
import { LimitsSection } from "./LimitsSection";

const card = "rounded-overlay border border-border bg-surface p-5 shadow-1";
const cell = "border-b border-border px-2 py-1 text-left";

/** Tokens and cost of one day or span, in words a table cell holds. */
function Figures({ day }: { day: UsageDay }) {
  return (
    <dl className="grid grid-cols-[max-content_1fr] gap-x-4 gap-y-1 text-sm">
      <dt className="text-ink-muted">{s.usage.tokensIn}</dt>
      <dd className="text-ink">{day.inputTokens}</dd>
      <dt className="text-ink-muted">{s.usage.tokensOut}</dt>
      <dd className="text-ink">{day.outputTokens}</dd>
      <dt className="text-ink-muted">{s.usage.cost}</dt>
      <dd className="text-ink">{s.usage.dollars(day.cost)}</dd>
    </dl>
  );
}

function limitText(value: number | null | undefined, money = false): string {
  if (value === null || value === undefined) return s.usage.noLimit;
  return money ? s.usage.dollars(value) : String(value);
}

function YourLimits({ limits }: { limits: Limits }) {
  return (
    <dl className="grid grid-cols-[max-content_1fr] gap-x-4 gap-y-1 text-sm" data-your-limits>
      <dt className="text-ink-muted">{s.usage.dailyTokens}</dt>
      <dd className="text-ink">{limitText(limits.dailyTokens)}</dd>
      <dt className="text-ink-muted">{s.usage.dailyCost}</dt>
      <dd className="text-ink">{limitText(limits.dailyCost, true)}</dd>
      <dt className="text-ink-muted">{s.usage.concurrentRuns}</dt>
      <dd className="text-ink">{limitText(limits.concurrentRuns)}</dd>
    </dl>
  );
}

/** What the person's runs used, the limits on them and, for administrators, everyone's usage and the limits to set. */
export function UsagePage() {
  const me = useQuery(meQuery);
  const mine = useQuery(myUsageQuery);
  const admin = me.data?.role === "admin";
  return (
    <div className="flex flex-col gap-6">
      <PageHeader title={s.usage.title} />
      <p className="max-w-2xl text-sm text-ink-muted">{s.usage.body}</p>
      {mine.isPending && <p className="text-sm text-ink-muted">{s.usage.loading}</p>}
      {mine.isError && (
        <p role="alert" className="text-sm text-danger">
          {mine.error.message}
        </p>
      )}
      {mine.data && (
        <div className="grid max-w-4xl gap-4 md:grid-cols-3" data-usage>
          <section className={card} aria-labelledby="usage-today">
            <h2 id="usage-today" className="mb-2 font-semibold text-ink">
              {s.usage.today}
            </h2>
            <Figures day={mine.data.today} />
          </section>
          <section className={card} aria-labelledby="usage-month">
            <h2 id="usage-month" className="mb-2 font-semibold text-ink">
              {s.usage.month}
            </h2>
            <Figures day={mine.data.month} />
          </section>
          <section className={card} aria-labelledby="usage-limits">
            <h2 id="usage-limits" className="mb-2 font-semibold text-ink">
              {s.usage.yourLimits}
            </h2>
            <YourLimits limits={mine.data.limits} />
          </section>
        </div>
      )}
      {mine.data && (
        <section className={`${card} max-w-4xl overflow-x-auto`} aria-labelledby="usage-days">
          <h2 id="usage-days" className="mb-2 font-semibold text-ink">
            {s.usage.days}
          </h2>
          {mine.data.days.length === 0 ? (
            <p className="text-sm text-ink-muted">{s.usage.none}</p>
          ) : (
            <table className="w-full text-sm text-ink" data-usage-days>
              <thead>
                <tr>
                  <th className={cell}>{s.usage.day}</th>
                  <th className={cell}>{s.usage.tokensIn}</th>
                  <th className={cell}>{s.usage.tokensOut}</th>
                  <th className={cell}>{s.usage.cost}</th>
                </tr>
              </thead>
              <tbody>
                {mine.data.days.map((d) => (
                  <tr key={d.day}>
                    <td className={cell}>{d.day}</td>
                    <td className={cell}>{d.inputTokens}</td>
                    <td className={cell}>{d.outputTokens}</td>
                    <td className={cell}>{s.usage.dollars(d.cost)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </section>
      )}
      {admin && mine.data && <LimitsSection limits={mine.data.limits} />}
      {admin && <People />}
    </div>
  );
}

function People() {
  const people = useQuery(peopleUsageQuery);
  return (
    <section className={`${card} max-w-4xl overflow-x-auto`} aria-labelledby="usage-people">
      <h2 id="usage-people" className="mb-2 font-semibold text-ink">
        {s.usage.peopleTitle}
      </h2>
      {people.data?.length === 0 && <p className="text-sm text-ink-muted">{s.usage.none}</p>}
      {people.data && people.data.length > 0 && (
        <table className="w-full text-sm text-ink" data-usage-people>
          <thead>
            <tr>
              <th className={cell}>{s.usage.person}</th>
              <th className={cell}>{s.usage.today}</th>
              <th className={cell}>{s.usage.month}</th>
            </tr>
          </thead>
          <tbody>
            {people.data.map((p) => (
              <tr key={p.person.id}>
                <td className={cell}>{p.person.name}</td>
                <td className={cell}>
                  {p.today.inputTokens + p.today.outputTokens} . {s.usage.dollars(p.today.cost)}
                </td>
                <td className={cell}>
                  {p.month.inputTokens + p.month.outputTokens} . {s.usage.dollars(p.month.cost)}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  );
}
