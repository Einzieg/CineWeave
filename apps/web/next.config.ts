import type { NextConfig } from "next";
import { resolveBuildId } from "./build-id.mjs";

const nextConfig: NextConfig = {
  output: "standalone",
  typedRoutes: true,
  generateBuildId: async () => resolveBuildId(),
};

export default nextConfig;
