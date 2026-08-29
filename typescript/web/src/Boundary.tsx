import { Component, type ErrorInfo, type ReactNode } from 'react'

// The boundary a crash stops at.
//
// On 28 August the live /live showed a WHITE SCREEN: the handler returned
// `candidates: null`, the page took a length on it and died outright. The data is
// fixed, but that is not the lesson: a page a judge opens by hand has no right to
// disappear over one bad field. A section falls, the rest stays readable.
//
// The error is NOT hidden by this: it is printed on the page. Emptiness put there
// silently is worse than a visible failure - it is taken for "there is no data".
type Props = { children: ReactNode; says: string }
type State = { trouble: Error | null }

export class Boundary extends Component<Props, State> {
  state: State = { trouble: null }

  static getDerivedStateFromError(trouble: Error): State {
    return { trouble }
  }

  componentDidCatch(trouble: Error, where: ErrorInfo): void {
    console.error('section failed:', this.props.says, trouble, where.componentStack)
  }

  render(): ReactNode {
    if (!this.state.trouble) return this.props.children

    return (
      <p className="m-0 rounded-xl border border-dashed border-loss/50 px-6 py-5 text-[15px] text-loss">
        {this.props.says}: {this.state.trouble.message}
      </p>
    )
  }
}
