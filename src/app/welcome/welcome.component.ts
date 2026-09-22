import { ChangeDetectorRef, Component, EventEmitter, OnDestroy, OnInit, Output, ViewChild } from '@angular/core';
import { PluginListenerHandle } from '@capacitor/core';
import { IonAccordionGroup, IonicModule} from '@ionic/angular';
import GoBridge, { GoInterface } from '../plugins/go.bridge';
import JavaBridge from '../plugins/java.bridge';
import { RegistryState } from '../services/updater.types';
import { CommonModule, LocationStrategy } from '@angular/common';


enum Slides {
    Welcome = 0,
    Permissions = 1,
    Download = 2,
    Continue = 3,
}

@Component({
  selector: 'welcome-screen',
  standalone: true,
  templateUrl: './welcome.component.html',
  styleUrls: ['./welcome.component.scss'],
  imports: [CommonModule, IonicModule]
})
export class WelcomeComponent implements OnInit, OnDestroy {
  step = Slides.Welcome;
  @ViewChild('permissionsGroup', { static: true }) permissionsGroup: IonAccordionGroup;

  IsOnWifi: boolean = false;

  NotificationPermissionGranted: boolean = false;
  VPNPermissionGranted: boolean = false;
  private readonly onVPNPermission = (event: Event) => {
    this.VPNPermissionGranted = Boolean((event as any).granted ?? (event as CustomEvent).detail?.granted);
    this.changeDetector.detectChanges();
  };

  constructor(private changeDetector: ChangeDetectorRef, private locationStrategy: LocationStrategy) { }

  public async ngOnInit() {
    var result = await JavaBridge.isNotificationPermissionGranted();
    this.NotificationPermissionGranted = result.granted;

    result = await JavaBridge.isVPNPermissionGranted();
    this.VPNPermissionGranted = result.granted;

    window.addEventListener("vpn-permission", this.onVPNPermission);
    this.permissionsGroup.value = "vpn";

    GoBridge.IsOnWifiNetwork().then((onWifi) => {
      this.IsOnWifi = onWifi;
    });
  }

  ngOnDestroy(): void {
    window.removeEventListener("vpn-permission", this.onVPNPermission);
  }

  public Download() {
    this.step = Slides.Continue;
    GoBridge.DownloadPendingUpdates();
    JavaBridge.setWelcomeScreenShowed({showed: true});
  }

  public WaitForWifi() {
    this.step = Slides.Continue;
    GoBridge.DownloadUpdatesOnWifiConnected();
    JavaBridge.setWelcomeScreenShowed({showed: true});
  } 

  public Continue() {
    this.locationStrategy.back();
  }

  public RequestVPNPermission() {
    JavaBridge.requestVPNPermission();
    this.permissionsGroup.value = "notifications";
  }

  public async RequestNotificationPermission() {
    var result = await JavaBridge.requestNotificationsPermission();
    this.NotificationPermissionGranted = result.granted;
    this.permissionsGroup.value = "apps";
  }

  public NetworkState() {
    this.permissionsGroup.value = "netstate";
  }

  public NextSlide() {
    if (this.step === Slides.Welcome || (this.step === Slides.Permissions && this.VPNPermissionGranted)) {
      this.step++;
    }
  }
}
