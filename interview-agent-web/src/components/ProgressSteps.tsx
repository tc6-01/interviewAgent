import { CheckIcon } from "./Icons";

const steps = ["资料准备", "方向确认", "模拟面试", "评估复盘"];

export function ProgressSteps({ current }: { current: number }) {
  return (
    <nav className="progress-steps" aria-label="面试流程">
      {steps.map((step, index) => {
        const number = index + 1;
        const complete = number < current;
        const active = number === current;
        return (
          <div
            className={`progress-step${active ? " is-active" : ""}${complete ? " is-complete" : ""}`}
            key={step}
            aria-current={active ? "step" : undefined}
          >
            <span className="progress-step__index">{complete ? <CheckIcon /> : number}</span>
            <span>{step}</span>
          </div>
        );
      })}
    </nav>
  );
}
