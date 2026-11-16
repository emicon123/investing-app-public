/**
 * Confirm-dialog consequence line shared by the three apps' delete dialogs (task 173, ADR-109).
 * Delete is a soft delete: the row only leaves the portfolio overview, its history stays, and
 * re-adding the same ticker restores it with its old holdings.
 */
export const SOFT_DELETE_CONSEQUENCE =
  "Zniknie z przeglądu portfela, historia transakcji zostaje. Ponowne dodanie tego samego tickera przywróci pozycję wraz ze stanem posiadania.";
