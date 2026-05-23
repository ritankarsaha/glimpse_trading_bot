/** @type {import('next').NextConfig} */
const nextConfig = {
  async rewrites() {
    const api = process.env.API_URL ?? 'http://localhost:8080';
    return [
      {
        source: '/api/:path*',
        destination: `${api}/api/:path*`,
      },
    ];
  },
};

export default nextConfig;
