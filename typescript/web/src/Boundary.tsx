import { Component, type ErrorInfo, type ReactNode } from 'react'

// Граница падения.
//
// 28 августа боевая /live показала БЕЛЫЙ ЭКРАН: обработчик отдал `candidates:
// null`, страница взяла по нему длину и умерла целиком. Данные починены, но урок
// не в этом: страница, которую судья открывает руками, не имеет права исчезать
// из-за одного плохого поля. Падает раздел - остальное читается.
//
// Ошибка при этом НЕ прячется: она печатается на странице. Молчаливо
// подставленная пустота хуже видимого отказа - её принимают за «данных нет».
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
