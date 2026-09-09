package io.safing.portmaster.android.connectivity;

import android.net.ConnectivityManager;
import android.net.Network;
import android.net.NetworkCapabilities;

import androidx.annotation.NonNull;

import engine.Engine;
import io.safing.portmaster.android.os.NetworkProxy;

public class NetworkCallbacks extends ConnectivityManager.NetworkCallback {
  private final PortmasterTunnelService service;

  public NetworkCallbacks(PortmasterTunnelService service) {
    this.service = service;
  }

  @Override
  public void onAvailable(@NonNull Network network) {
    super.onAvailable(network);
    Engine.onNetworkConnected();
    service.onUnderlyingNetworkAvailable(network);
  }

  @Override
  public void onLost(@NonNull Network network) {
    super.onLost(network);
    Engine.onNetworkDisconnected();
    service.onUnderlyingNetworkLost(network);
  }

  @Override
  public void onCapabilitiesChanged(
      @NonNull Network network,
      @NonNull NetworkCapabilities networkCapabilities) {
    super.onCapabilitiesChanged(network, networkCapabilities);
    Engine.onNetworkCapabilitiesChanged(new NetworkProxy(networkCapabilities));
    service.onUnderlyingNetworkCapabilitiesChanged(network, networkCapabilities);
  }
}
