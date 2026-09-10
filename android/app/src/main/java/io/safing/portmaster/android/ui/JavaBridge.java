package io.safing.portmaster.android.ui;

import android.content.Intent;
import android.content.pm.ApplicationInfo;
import android.content.pm.PackageManager;
import android.net.Uri;
import android.net.VpnService;

import com.getcapacitor.JSArray;
import com.getcapacitor.JSObject;
import com.getcapacitor.Plugin;
import com.getcapacitor.PluginCall;
import com.getcapacitor.PluginMethod;
import com.getcapacitor.annotation.CapacitorPlugin;

import org.json.JSONArray;
import org.json.JSONException;
import org.json.JSONObject;

import java.util.HashSet;
import java.util.List;
import java.util.Locale;
import java.util.Set;

import io.safing.portmaster.android.settings.Settings;

@CapacitorPlugin(name = "JavaBridge")
public class JavaBridge extends Plugin {

  @PluginMethod()
  public void getAppSettings(PluginCall call) {
    final PackageManager pm = getActivity().getPackageManager();
    Set<String> disabledApps = Settings.getDisabledApps(getActivity());

    List<ApplicationInfo> packages = pm.getInstalledApplications(0);
    JSONArray list = new JSONArray();
    for (ApplicationInfo packageInfo : packages) {
      JSONObject obj = new JSONObject();
      try {
        obj.put("name", pm.getApplicationLabel(packageInfo).toString());
        obj.put("packageName", packageInfo.packageName);
        obj.put("enabled", !disabledApps.contains(packageInfo.packageName));
        obj.put("system", isSystemPackage(packageInfo));
      } catch (JSONException e) {
        call.reject("Failed to enumerate installed applications", e);
        return;
      }
      list.put(obj);
    }
    JSObject obj = new JSObject();
    obj.put("apps", list);
    call.resolve(obj);
  }

  private boolean isSystemPackage(ApplicationInfo info) {
    return ((info.flags & ApplicationInfo.FLAG_SYSTEM) != 0);
  }

  @PluginMethod()
  public void setAppSettings(PluginCall call) {
    JSArray array = call.getArray("apps");
    if (array == null) {
      call.reject("Missing apps array");
      return;
    }

    final PackageManager pm = getActivity().getPackageManager();
    Set<String> disabledPackages = new HashSet<>();

    try {
      List<String> apps = array.toList();
      for (String packageName : apps) {
        if (packageName == null || packageName.isBlank()) {
          call.reject("Invalid empty package name");
          return;
        }

        // Only persist a bypass for a package that actually exists. This keeps
        // malformed/untrusted bridge input from accumulating arbitrary entries.
        try {
          pm.getApplicationInfo(packageName, 0);
        } catch (PackageManager.NameNotFoundException e) {
          call.reject("Unknown application package: " + packageName);
          return;
        }

        // Never let Portmaster itself be excluded from its own VpnService.
        if (packageName.equals(getActivity().getPackageName())) {
          continue;
        }
        disabledPackages.add(packageName);
      }

      Settings.setDisabledApps(getActivity(), disabledPackages);
      call.resolve();
    } catch (JSONException | ClassCastException e) {
      call.reject("Invalid apps array", e);
    }
  }

  @PluginMethod()
  public void requestVPNPermission(PluginCall call) {
    MainActivity activity = (MainActivity) getActivity();
    Intent intent = VpnService.prepare(activity.getApplicationContext());
    if(intent != null) {
      activity.startActivityForResult(intent, MainActivity.REQUEST_VPN_PERMISSION);
    }
    call.resolve();
  }

  @PluginMethod
  public void isVPNPermissionGranted(PluginCall call) throws JSONException {
    Intent intent = VpnService.prepare(getActivity().getApplicationContext());
    if(intent != null) {
      call.resolve(new JSObject("{\"granted\": false}"));
    } else {
      call.resolve(new JSObject("{\"granted\": true}"));
    }
  }

  @PluginMethod
  public void requestNotificationsPermission(PluginCall call) throws JSONException {
    MainActivity activity = (MainActivity) getActivity();
    activity.createNotificationChannel();
    call.resolve(new JSObject(String.format("{\"granted\": %s}", activity.isNotificationChannelCreated())));
  }

  @PluginMethod
  public void isNotificationPermissionGranted(PluginCall call) throws JSONException {
    MainActivity activity = (MainActivity) getActivity();
    call.resolve(new JSObject(String.format("{\"granted\": %s}", activity.isNotificationChannelCreated())));
  }

  @PluginMethod
  public void initEngine(PluginCall call) {
    MainActivity activity = (MainActivity) getActivity();
    activity.initEngine();
    call.resolve();
    Settings.setWelcomeScreenShowed(activity, true);
  }

  @PluginMethod
  public void shouldShowWelcomeScreen(PluginCall call) throws JSONException {
    boolean should = Settings.ShouldShowWelcomeScreen(getActivity());
    call.resolve(new JSObject(String.format("{\"show\": %s}", should)));
  }

  @PluginMethod
  public void openUrlInBrowser(PluginCall call) {
    String url = call.getString("url");
    if (url == null) {
      call.reject("Missing URL");
      return;
    }

    try {
      Uri uri = Uri.parse(url);
      String scheme = uri.getScheme();
      if (scheme == null || !"https".equals(scheme.toLowerCase(Locale.ROOT))) {
        call.reject("Only HTTPS URLs may be opened");
        return;
      }

      Intent browserIntent = new Intent(Intent.ACTION_VIEW, uri);
      this.getActivity().startActivity(browserIntent);
      call.resolve();
    } catch(Exception e) {
      call.reject("Failed to open URL", e);
    }
  }

  @PluginMethod
  public void setWelcomeScreenShowed(PluginCall call) {
    try {
      boolean showed = call.getBoolean("showed");
      Settings.setWelcomeScreenShowed(getActivity(), showed);
      call.resolve();
    } catch(Exception e) {
      call.reject("Failed to update welcome-screen setting", e);
    }
  }

  @PluginMethod
  public void openVPNSettings(PluginCall call) {
    getActivity().startActivity(new Intent(android.provider.Settings.ACTION_VPN_SETTINGS));
    call.resolve();
  }
}
