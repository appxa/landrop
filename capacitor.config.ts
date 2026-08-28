import { CapacitorConfig } from '@capacitor/cli';

const config: CapacitorConfig = {
  appId: 'io.github.appxa.landrop',
  appName: 'lanDrop',
  webDir: 'public',
  android: {
    allowMixedContent: true,
  },
  plugins: {
    Nodejs: {
      nodeDir: 'nodejs',
      startMode: 'auto',
    },
  },
};

export default config;