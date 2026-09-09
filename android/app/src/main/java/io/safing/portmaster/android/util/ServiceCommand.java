package io.safing.portmaster.android.util;

import android.content.Intent;
import android.net.VpnService;

import androidx.core.content.ContextCompat;

import io.safing.portmaster.android.connectivity.PortmasterTunnelService;
import io.safing.portmaster.android.go_interface.Function;
import io.safing.portmaster.android.ui.MainActivity;

public class ServiceCommand extends Function {

  private MainActivity activity;

  public ServiceCommand(String name, MainActivity activity) {
    super(name);
    this.activity = activity;
  }

  @Override
  public byte[] call(byte[] args) throws Exception {
    String command = parseArguments(args, String.class);
    this.send(command);
    return null;
  }

  public void send(String command) {
    Intent intent = VpnService.prepare(activity.getApplicationContext());
    if(intent != null) {
      intent.putExtra("command", command);
      activity.startActivityForResult(intent, MainActivity.ENABLE_VPN);
      return;
    }

    intent = new Intent(activity, PortmasterTunnelService.class);
    intent.setAction(PortmasterTunnelService.COMMAND_PREFIX + command);
    ContextCompat.startForegroundService(activity, intent);
  }
}
