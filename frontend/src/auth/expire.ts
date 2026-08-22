let expiring = false;

// A rejected credential cannot be recovered in place, so hand the Owner back to the
// login flow instead of leaving a shell whose every request fails. The existing logout
// route already revokes the credential, clears the cookie, and redirects, and a form
// POST lets the browser follow that redirect. Inert outside the browser, so server
// rendering and the Node test suite keep mapping 401 to a message only.
export function expireSession() {
  if (expiring || typeof window === "undefined") return;
  expiring = true;
  const form = document.createElement("form");
  form.method = "POST";
  form.action = "/api/auth/logout";
  document.body.appendChild(form);
  form.submit();
}
