import { Component, Input, Output, EventEmitter } from '@angular/core';

import { CommonModule, LocationStrategy } from '@angular/common';
import { IonicModule } from '@ionic/angular';
import { FormsModule } from '@angular/forms';
import { SPNService } from '../lib/spn.service';
import GoBridge from '../plugins/go.bridge';
import JavaBridge from '../plugins/java.bridge';
// import { SPNService } from '@safing/portmaster-api/src/lib/spn.service';


@Component({
  selector: 'app-login-container',
  templateUrl: './login.component.html',
  styleUrls: ['./login.component.scss'],
  standalone: true,
  imports: [CommonModule, FormsModule, IonicModule]
})
export class LoginComponent {
  Error: string;
  Loading: boolean = false;
  
  Username: string
  Password: string

  ShowPassword: boolean
  PasswordFieldType: "password" | "text";

  constructor(
    private spnService: SPNService, 
    private location: LocationStrategy) { 
    this.PasswordFieldType = "password";
  }

  login() {
    if (this.Loading) {
      return;
    }
    this.Error = "";
    this.Loading = true;

    this.spnService.login({username: this.Username, password: this.Password})
      .subscribe({
        next: async () => {
          try {
            if (!(await GoBridge.IsTunnelActive())) {
              await GoBridge.EnableTunnel();
            }
          } finally {
            this.Loading = false;
            this.Password = "";
            this.location.back();
          }
        },
        error: err => {
          this.Loading = false;
          this.Error = err?.message || err?.errorMessage || String(err);
        },
      });
  }

  openSignup() {
    JavaBridge.openUrlInBrowser({url: "https://account.safing.io/account/sign_up"});
  }

  async togglePasswordVisibility(): Promise<void> {
    this.ShowPassword = !this.ShowPassword;
    if(this.ShowPassword) {
      this.PasswordFieldType = "text";
    } else {
      this.PasswordFieldType = "password";
    }
  }
}
