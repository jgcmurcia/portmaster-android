package io.safing.portmaster.android.connectivity;

import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.content.BroadcastReceiver;
import android.content.Context;
import android.content.Intent;
import android.content.IntentFilter;
import android.content.pm.PackageManager;
import android.net.ConnectivityManager;
import android.net.Network;
import android.net.NetworkCapabilities;
import android.net.NetworkRequest;
import android.net.VpnService;
import android.os.Build;
import android.os.Handler;
import android.os.Looper;
import android.os.ParcelFileDescriptor;
import android.os.PowerManager;
import android.util.Log;

import androidx.core.app.NotificationCompat;

import java.net.DatagramSocket;
import java.util.LinkedHashSet;
import java.util.Set;

import engine.Engine;
import io.safing.portmaster.android.R;
import io.safing.portmaster.android.go_interface.Function;
import io.safing.portmaster.android.go_interface.GoInterface;
import io.safing.portmaster.android.os.OSFunctions;
import io.safing.portmaster.android.receiver.SystemIdleEventReceiver;
import io.safing.portmaster.android.settings.Settings;
import io.safing.portmaster.android.ui.MainActivity;
import io.safing.portmaster.android.util.CancelNotification;
import io.safing.portmaster.android.util.ConnectionOwner;
import io.safing.portmaster.android.util.GetAppUID;
import io.safing.portmaster.android.util.ShowNotification;
import io.safing.portmaster.android.util.VPNInit;
import io.safing.portmaster.android.util.VPNProtect;
import tunnel.Tunnel;

public class PortmasterTunnelService extends VpnService {

  public static final String COMMAND_PREFIX = "io.safing.portmaster.tunnel.";
  public static final String ACTION_KEEP_ALIVE = COMMAND_PREFIX + "keep_alive";
  public static final String ACTION_SHUTDOWN = COMMAND_PREFIX + "shutdown";

  private static final String TAG = "PortmasterTunnelService";
  private static final String VPN_CHANNEL_ID = "PortmasterVPN";
  private static final int VPN_NOTIFICATION_ID = 1001;
  private static final int VPN_MTU = 1400;
  private static final long NETWORK_HANDOFF_DELAY_MS = 750L;

  private PendingIntent mConfigureIntent;

  private BroadcastReceiver systemIdleEventReceiver;
  private ConnectivityManager.NetworkCallback networkCallback;
  private ConnectivityManager connectivityManager;

  private final Handler networkHandler = new Handler(Looper.getMainLooper());
  private final Object networkLock = new Object();
  private final Set<Network> physicalNetworks = new LinkedHashSet<>();
  private volatile boolean tunnelRequested = false;
  private volatile boolean gracefulShutdown = false;

  private Function showNotification;
  private Function cancelNotification;
  private Function ignoreSocket;
  private Function connectionOwner;
  private Function vpnInit;
  private Function appUid;

  private final Runnable reconnectTunnel = () -> {
    if (!tunnelRequested || gracefulShutdown) {
      return;
    }

    synchronized (networkLock) {
      if (physicalNetworks.isEmpty()) {
        Log.i(TAG, "network handoff pending: no physical network available; keeping VPN fail-closed");
        return;
      }
    }

    Log.i(TAG, "physical network changed; rebuilding tunnel");
    Tunnel.reconnect();
  };

  @Override
  public boolean protect(DatagramSocket socket) {
    return super.protect(socket);
  }

  @Override
  public void onCreate() {
    super.onCreate();

    Engine.setOSFunctions(OSFunctions.get());
    connectivityManager = (ConnectivityManager) getSystemService(Context.CONNECTIVITY_SERVICE);
    createNotificationChannels();

    mConfigureIntent = PendingIntent.getActivity(
      this,
      0,
      new Intent(this, MainActivity.class),
      PendingIntent.FLAG_UPDATE_CURRENT | PendingIntent.FLAG_IMMUTABLE
    );

    registerEvents();

    GoInterface uiInterface = new GoInterface();
    this.vpnInit = new VPNInit("VPNInit", this);
    uiInterface.registerFunction(this.vpnInit);
    this.showNotification = new ShowNotification("ShowNotification", this);
    uiInterface.registerFunction(this.showNotification);
    this.cancelNotification = new CancelNotification("CancelNotification", this);
    uiInterface.registerFunction(this.cancelNotification);
    this.ignoreSocket = new VPNProtect("IgnoreSocket", this);
    uiInterface.registerFunction(this.ignoreSocket);
    this.connectionOwner = new ConnectionOwner("GetConnectionOwner", connectivityManager);
    uiInterface.registerFunction(this.connectionOwner);
    this.appUid = new GetAppUID("GetAppUID", this);
    uiInterface.registerFunction(this.appUid);

    Engine.setServiceFunctions(uiInterface);

    Log.v(TAG, "Engine.onCreate from VPN service");
    Engine.onCreate(this.getFilesDir().getAbsolutePath());
  }

  @Override
  public int onStartCommand(Intent intent, int flags, int startId) {
    ensureForeground();

    if (intent != null && ACTION_SHUTDOWN.equals(intent.getAction())) {
      Log.i(TAG, "graceful VPN service shutdown requested");
      gracefulShutdown = true;
      tunnelRequested = false;
      networkHandler.removeCallbacks(reconnectTunnel);
      stopSelf(startId);
      return START_NOT_STICKY;
    }

    gracefulShutdown = false;
    tunnelRequested = true;
    Tunnel.enable();
    return START_STICKY;
  }

  @Override
  public void onDestroy() {
    tunnelRequested = false;
    networkHandler.removeCallbacks(reconnectTunnel);
    unregisterSystemEvents();

    if (gracefulShutdown) {
      Engine.onServiceStop();
    } else {
      Engine.onServiceDestroy();
    }

    stopForeground(true);
    super.onDestroy();
  }

  @Override
  public void onRevoke() {
    Log.w(TAG, "VPN permission revoked; tearing down tunnel");
    gracefulShutdown = true;
    tunnelRequested = false;
    networkHandler.removeCallbacks(reconnectTunnel);
    Tunnel.disable();
    networkHandler.postDelayed(this::stopSelf, 1000L);
  }

  public int InitVPN() {
    Builder builder = this.new Builder()
      .setMtu(VPN_MTU)
      .addAddress("100.127.247.245", 30)
      .addAddress("fd00:1::1", 64)
      .addRoute("0.0.0.0", 0)
      .addRoute("::", 0)
      .addDnsServer("9.9.9.9")
      .addDnsServer("2620:fe::fe");

    Set<String> disabledPackages = Settings.getDisabledApps(this);
    for (String packageName : disabledPackages) {
      try {
        builder.addDisallowedApplication(packageName);
      } catch (PackageManager.NameNotFoundException e) {
        Log.w(TAG, "disabled package disappeared: " + packageName, e);
      }
    }

    builder.setSession("Portmaster");
    builder.setConfigureIntent(mConfigureIntent);

    if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
      builder.setMetered(false);
    }

    synchronized (this) {
      ParcelFileDescriptor fd = builder.establish();
      if (fd != null) {
        return fd.detachFd();
      }
    }

    Log.e(TAG, "VpnService.Builder.establish returned null");
    return -1;
  }

  public void onUnderlyingNetworkCapabilitiesChanged(
      Network network,
      NetworkCapabilities capabilities) {
    if (capabilities.hasTransport(NetworkCapabilities.TRANSPORT_VPN)) {
      return;
    }

    boolean changed;
    synchronized (networkLock) {
      changed = physicalNetworks.add(network);
    }

    if (changed && tunnelRequested) {
      scheduleTunnelReconnect();
    }
  }

  public void onUnderlyingNetworkLost(Network network) {
    boolean changed;
    boolean anyRemaining;
    synchronized (networkLock) {
      changed = physicalNetworks.remove(network);
      anyRemaining = !physicalNetworks.isEmpty();
    }

    // Protected Go sockets intentionally follow Android's current default
    // physical network; they are not bound to a specific Network object. The
    // callback set is therefore used only to detect handoffs/reconnect SPN.
    if (changed && anyRemaining && tunnelRequested) {
      scheduleTunnelReconnect();
    }
  }

  private void scheduleTunnelReconnect() {
    networkHandler.removeCallbacks(reconnectTunnel);
    networkHandler.postDelayed(reconnectTunnel, NETWORK_HANDOFF_DELAY_MS);
  }

  private void ensureForeground() {
    Intent openApp = new Intent(this, MainActivity.class);
    PendingIntent contentIntent = PendingIntent.getActivity(
      this,
      1,
      openApp,
      PendingIntent.FLAG_UPDATE_CURRENT | PendingIntent.FLAG_IMMUTABLE
    );

    Notification notification = new NotificationCompat.Builder(this, VPN_CHANNEL_ID)
      .setSmallIcon(R.drawable.notify_icon)
      .setContentTitle(getString(R.string.vpn_notification_title))
      .setContentText(getString(R.string.vpn_notification_text))
      .setContentIntent(contentIntent)
      .setCategory(NotificationCompat.CATEGORY_SERVICE)
      .setPriority(NotificationCompat.PRIORITY_LOW)
      .setOngoing(true)
      .setOnlyAlertOnce(true)
      .build();

    startForeground(VPN_NOTIFICATION_ID, notification);
  }

  private void createNotificationChannels() {
    if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) {
      return;
    }

    NotificationManager notificationManager = getSystemService(NotificationManager.class);

    NotificationChannel appChannel = new NotificationChannel(
      MainActivity.CHANNEL_ID,
      getString(R.string.notification_channel_name),
      NotificationManager.IMPORTANCE_DEFAULT
    );
    appChannel.setDescription(getString(R.string.notification_channel_description));
    notificationManager.createNotificationChannel(appChannel);

    NotificationChannel vpnChannel = new NotificationChannel(
      VPN_CHANNEL_ID,
      getString(R.string.vpn_notification_channel_name),
      NotificationManager.IMPORTANCE_LOW
    );
    vpnChannel.setDescription(getString(R.string.vpn_notification_channel_description));
    vpnChannel.setShowBadge(false);
    notificationManager.createNotificationChannel(vpnChannel);
  }

  private void registerEvents() {
    systemIdleEventReceiver = new SystemIdleEventReceiver();
    IntentFilter filter = new IntentFilter(PowerManager.ACTION_DEVICE_IDLE_MODE_CHANGED);
    this.registerReceiver(systemIdleEventReceiver, filter);

    if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.N && connectivityManager != null) {
      Network active = connectivityManager.getActiveNetwork();
      if (active != null) {
        NetworkCapabilities capabilities = connectivityManager.getNetworkCapabilities(active);
        if (capabilities == null || !capabilities.hasTransport(NetworkCapabilities.TRANSPORT_VPN)) {
          synchronized (networkLock) {
            physicalNetworks.add(active);
          }
        }
      }

      networkCallback = new NetworkCallbacks(this);
      NetworkRequest networkRequest = new NetworkRequest.Builder()
        .addCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)
        .addCapability(NetworkCapabilities.NET_CAPABILITY_NOT_VPN)
        .addTransportType(NetworkCapabilities.TRANSPORT_WIFI)
        .addTransportType(NetworkCapabilities.TRANSPORT_CELLULAR)
        .addTransportType(NetworkCapabilities.TRANSPORT_ETHERNET)
        .build();

      connectivityManager.registerNetworkCallback(networkRequest, networkCallback);
    }
  }

  private void unregisterSystemEvents() {
    if (systemIdleEventReceiver != null) {
      try {
        this.unregisterReceiver(systemIdleEventReceiver);
      } catch (IllegalArgumentException ignored) {
        // Receiver may already have been removed during process teardown.
      }
      systemIdleEventReceiver = null;
    }

    if (networkCallback != null && Build.VERSION.SDK_INT >= Build.VERSION_CODES.N) {
      try {
        connectivityManager.unregisterNetworkCallback(networkCallback);
      } catch (IllegalArgumentException ignored) {
        // Callback may already be gone if ConnectivityService restarted.
      }
      networkCallback = null;
    }

    synchronized (networkLock) {
      physicalNetworks.clear();
    }
  }
}
