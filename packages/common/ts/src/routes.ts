export const ROUTES = {
  portfolio: "/",
  purchases: "/purchases",
  deposits: "/deposits",
  history: "/history",
  settings: "/settings",
} as const;

export type AppRoute = (typeof ROUTES)[keyof typeof ROUTES];
