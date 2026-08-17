import { useEffect, useState } from "react";

export type AppRoute =
  | { name: "setup" }
  | { name: "interview"; interviewId: string }
  | { name: "results"; interviewId: string }
  | { name: "not-found" };

function readRoute(): AppRoute {
  const hash = window.location.hash.replace(/^#/, "") || "/setup";
  const interview = hash.match(/^\/interview\/([^/]+)$/);
  if (interview) return { name: "interview", interviewId: decodeURIComponent(interview[1]) };
  const results = hash.match(/^\/results\/([^/]+)$/);
  if (results) return { name: "results", interviewId: decodeURIComponent(results[1]) };
  if (hash === "/" || hash === "/setup") return { name: "setup" };
  return { name: "not-found" };
}

export function useHashRoute(): AppRoute {
  const [route, setRoute] = useState(readRoute);
  useEffect(() => {
    const onChange = () => setRoute(readRoute());
    window.addEventListener("hashchange", onChange);
    return () => window.removeEventListener("hashchange", onChange);
  }, []);
  return route;
}

export function navigate(path: string): void {
  window.location.hash = path;
}
