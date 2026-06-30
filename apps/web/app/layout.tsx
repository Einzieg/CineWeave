import type { Metadata } from "next";
import { StudioSessionProvider } from "@/lib/session";
import "./globals.css";

export const metadata: Metadata = {
  title: "影织",
  description: "AI 视频创作工作台",
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="zh-CN">
      <body>
        <StudioSessionProvider>{children}</StudioSessionProvider>
      </body>
    </html>
  );
}
