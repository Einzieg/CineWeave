import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "CineWeave Studio",
  description: "CineWeave cloud native AI video production platform",
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="zh-CN">
      <body>{children}</body>
    </html>
  );
}

