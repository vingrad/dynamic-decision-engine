/** @type {import('next').NextConfig} */
const nextConfig = {
  // Produce a self-contained server build for a small Docker image.
  output: "standalone",
  // ESLint is intentionally not wired up in this minimal scaffold; type-checking
  // via tsc/next build is the quality gate. Type errors still fail the build.
  eslint: { ignoreDuringBuilds: true },
};

export default nextConfig;
