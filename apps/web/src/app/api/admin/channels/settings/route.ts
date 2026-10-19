import { proxyChannelSettings } from "@/lib/channel-admin";

export function GET() {
  return proxyChannelSettings();
}

export function POST(request: Request) {
  return proxyChannelSettings(request);
}
