import type { PlanVersion } from "@/lib/api";

// VersionList shows the immutable version history of a plan with an
// at-a-glance summary of the top move and its confidence in each version, so a
// reviewer can see how the recommendation evolved as signals arrived.
export function VersionList({
  versions,
  currentVersion,
}: {
  versions: PlanVersion[];
  currentVersion: number;
}) {
  if (!versions || versions.length === 0) {
    return <p className="empty">No versions yet.</p>;
  }
  return (
    <table>
      <thead>
        <tr>
          <th>Version</th>
          <th>Top move</th>
          <th>Top confidence</th>
          <th>Created</th>
        </tr>
      </thead>
      <tbody>
        {versions.map((v) => {
          const top = v.ranked_moves?.[0];
          return (
            <tr key={v.version}>
              <td>
                v{v.version}
                {v.version === currentVersion ? " · current" : ""}
              </td>
              <td>{top ? top.title : "—"}</td>
              <td className="mono">{top ? `${(top.confidence * 100).toFixed(0)}%` : "—"}</td>
              <td className="meta">{new Date(v.created_at).toLocaleString()}</td>
            </tr>
          );
        })}
      </tbody>
    </table>
  );
}
