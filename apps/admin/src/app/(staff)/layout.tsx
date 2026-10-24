import { redirect } from "next/navigation";
import type { ReactNode } from "react";

import { AdminShell } from "@/components/admin-shell";
import { ServiceUnavailable } from "@/components/service-unavailable";
import { StaffSessionProvider } from "@/components/staff-session";
import { getServerStaffSession } from "@/lib/server-session";

export const dynamic = "force-dynamic";

// Server gate for every staff page: Go decides, on each full render and
// refresh, whether the staff cookie is a live session. Pages under this group
// render only for an authenticated staff member; Go still authorizes every
// staff API call on its own.
export default async function StaffLayout({ children }: { children: ReactNode }) {
  const session = await getServerStaffSession();
  if (session.status === "anonymous") redirect("/login");
  if (session.status === "unavailable") return <ServiceUnavailable />;
  return (
    <StaffSessionProvider staff={session.staff}>
      <AdminShell>{children}</AdminShell>
    </StaffSessionProvider>
  );
}
