import { Eyebrow } from './parts'

// The judges' page. Deliberately EMPTY of claims for now - what belongs here is
// being decided, and a page that guesses at it would have to be unwritten later.
// It exists already so the link in the bar goes somewhere honest rather than
// bouncing a judge back to the landing with no explanation.
export function Submission() {
  return (
    <main className="mx-auto max-w-[1100px] px-6 pb-32 pt-16">
      <Eyebrow>[ for judges ]</Eyebrow>

      <h1 className="mt-6 max-w-[20ch] text-[40px] font-medium leading-[1.05] tracking-[-0.024em] text-primary">
        Everything a judge needs, in one place.
      </h1>

      <p className="mt-5 max-w-[58ch] text-[19px] leading-[1.35] text-secondary">
        The account, the rules it was held to, and the commands that recompute every number this
        project publishes — being assembled here.
      </p>

      <p className="mt-8 rounded-xl border border-dashed border-line px-6 py-5 text-[15px] text-muted">
        Not written yet. In the meantime the account is on{' '}
        <a className="text-accent underline underline-offset-4" href="/live">
          Live
        </a>{' '}
        and the result is at the foot of the{' '}
        <a className="text-accent underline underline-offset-4" href="/">
          overview
        </a>
        .
      </p>
    </main>
  )
}
