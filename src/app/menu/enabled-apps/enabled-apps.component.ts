import { Component, OnInit } from '@angular/core';

import { IonicModule } from '@ionic/angular';
import GoBridge from '../../plugins/go.bridge';
import JavaBridge from '../../plugins/java.bridge';

import { Application } from './application';
import { SystemAppList } from './enabled-apps.filter';
import { CommonModule, LocationStrategy } from '@angular/common';
import { FormsModule } from '@angular/forms';

@Component({
  selector: 'app-enabled-apps',
  templateUrl: './enabled-apps.component.html',
  styleUrls: ['./enabled-apps.component.scss'],
  standalone: true,
  imports: [CommonModule, IonicModule, FormsModule, SystemAppList]
})
export class EnabledAppsComponent implements OnInit {
  AppList: Application[] = [];
  Loading: boolean = true;
  Saving: boolean = false;
  Error: string = "";

  ShowSystemApps: boolean = false;

  constructor(private locationStrategy: LocationStrategy) {}

  async ngOnInit() {
    try {
      const result = await JavaBridge.getAppSettings();
      this.AppList = (result.apps || []).sort((a, b) => a.name.localeCompare(b.name));
    } catch (err) {
      this.Error = err?.message || String(err);
    } finally {
      this.Loading = false;
    }
  }

  async Save() {
    if (this.Saving) {
      return;
    }
    this.Saving = true;
    this.Error = "";

    const packageNameList = this.AppList
      .filter(app => !app.enabled)
      .map(app => app.packageName);

    try {
      await JavaBridge.setAppSettings({apps: packageNameList});
      await GoBridge.RestartTunnel();
      this.locationStrategy.back();
    } catch (err) {
      this.Error = err?.message || String(err);
    } finally {
      this.Saving = false;
    }
  }

  Close() {
    this.locationStrategy.back()
  }
}
