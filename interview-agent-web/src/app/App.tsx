import { useHashRoute } from "./router";
import { InterviewPage } from "../pages/InterviewPage";
import { ResultsPage } from "../pages/ResultsPage";
import { SetupPage } from "../pages/SetupPage";

export function App() {
  const route = useHashRoute();
  if (route.name === "interview") return <InterviewPage interviewId={route.interviewId} />;
  if (route.name === "results") return <ResultsPage interviewId={route.interviewId} />;
  return <SetupPage />;
}
