export namespace main {
	
	export class HopResult {
	    nr: number;
	    host: string;
	    loss: number;
	    sent: number;
	    recv: number;
	    best_ms: number;
	    avg_ms: number;
	    worst_ms: number;
	    last_ms: number;
	
	    static createFrom(source: any = {}) {
	        return new HopResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.nr = source["nr"];
	        this.host = source["host"];
	        this.loss = source["loss"];
	        this.sent = source["sent"];
	        this.recv = source["recv"];
	        this.best_ms = source["best_ms"];
	        this.avg_ms = source["avg_ms"];
	        this.worst_ms = source["worst_ms"];
	        this.last_ms = source["last_ms"];
	    }
	}
	export class DiagSession {
	    id: string;
	    host: string;
	    // Go type: time
	    started_at: any;
	    // Go type: time
	    ended_at: any;
	    hops: HopResult[];
	
	    static createFrom(source: any = {}) {
	        return new DiagSession(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.host = source["host"];
	        this.started_at = this.convertValues(source["started_at"], null);
	        this.ended_at = this.convertValues(source["ended_at"], null);
	        this.hops = this.convertValues(source["hops"], HopResult);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class LibreSpeedServer {
	    id: string;
	    name: string;
	    server: string;
	    dlURL: string;
	    ulURL: string;
	    pingURL: string;
	    country: string;
	
	    static createFrom(source: any = {}) {
	        return new LibreSpeedServer(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.server = source["server"];
	        this.dlURL = source["dlURL"];
	        this.ulURL = source["ulURL"];
	        this.pingURL = source["pingURL"];
	        this.country = source["country"];
	    }
	}
	export class NetworkInfo {
	    provider: string;
	    ip: string;
	    city: string;
	    country: string;
	    lat: number;
	    lon: number;
	
	    static createFrom(source: any = {}) {
	        return new NetworkInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.provider = source["provider"];
	        this.ip = source["ip"];
	        this.city = source["city"];
	        this.country = source["country"];
	        this.lat = source["lat"];
	        this.lon = source["lon"];
	    }
	}
	export class SpeedServer {
	    id: string;
	    name: string;
	    location: string;
	    flag: string;
	
	    static createFrom(source: any = {}) {
	        return new SpeedServer(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.location = source["location"];
	        this.flag = source["flag"];
	    }
	}
	export class SpeedtestSession {
	    id: string;
	    // Go type: time
	    started_at: any;
	    download_mbps: number;
	    upload_mbps: number;
	    ping_ms: number;
	    jitter_ms: number;
	    loss_pct: number;
	    server: string;
	    failed: boolean;
	    fail_reason?: string;
	
	    static createFrom(source: any = {}) {
	        return new SpeedtestSession(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.started_at = this.convertValues(source["started_at"], null);
	        this.download_mbps = source["download_mbps"];
	        this.upload_mbps = source["upload_mbps"];
	        this.ping_ms = source["ping_ms"];
	        this.jitter_ms = source["jitter_ms"];
	        this.loss_pct = source["loss_pct"];
	        this.server = source["server"];
	        this.failed = source["failed"];
	        this.fail_reason = source["fail_reason"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

