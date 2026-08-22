import { cookies } from "next/headers";
import { redirect } from "next/navigation";
import { ChainTraceApp } from "@/src/features/chaintrace/ChainTraceApp";
import { fetchOwnerIdentity, type Owner } from "@/src/auth/backend";
import { AUTH_COOKIE_NAME } from "@/src/auth/session";

export const dynamic = "force-dynamic";

export default async function Home() {
  const token = (await cookies()).get(AUTH_COOKIE_NAME)?.value;
  if (!token) redirect("/login");

  let owner: Owner | null = null;
  try {
    owner = (await fetchOwnerIdentity(token)).owner;
  } catch {
    // Unreachable identity services fail closed at the workspace boundary.
  }
  if (!owner) redirect("/login");

  return <ChainTraceApp owner={owner} />;
}
