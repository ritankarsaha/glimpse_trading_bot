/** @type {import('next').NextConfig} */
const nextConfig = {
  async rewrites() {
    const api = process.env.API_URL ?? 'http://localhost:3002';
    return {
      // beforeFiles runs BEFORE Next.js route resolution, so /api/* is
      // always proxied to the Go backend regardless of App Router's 404 handler.
      beforeFiles: [
        {
          source: '/api/:path*',
          destination: `${api}/api/:path*`,
        },
      ],
    };
  },
};

export default nextConfig;
