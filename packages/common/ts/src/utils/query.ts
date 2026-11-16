export function isAxiosNotFound(err: unknown): boolean {
  if (!err || typeof err !== "object") return false;
  if ("response" in err) {
    const response = (err as { response?: unknown }).response;
    if (response && typeof response === "object" && "status" in response) {
      return (response as { status?: unknown }).status === 404;
    }
  }
  if ("status" in err) {
    return (err as { status?: unknown }).status === 404;
  }
  return false;
}
