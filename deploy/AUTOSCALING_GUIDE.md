# Modigo Runner: AWS Auto Scaling Guide

Because the Modigo runner is completely **stateless**, setting up auto-scaling on AWS is straightforward. AWS will automatically spin up new servers when your CPU usage goes up (e.g., during a large group assessment) and shut them down when they are no longer needed to save you money.

Here is the step-by-step architecture to set this up.

---

## The Architecture
Instead of routing internet traffic directly to one EC2 instance, you will put an **Application Load Balancer (ALB)** in front. 
1. **Users** -> **AWS ALB (handles HTTPS)** -> **Auto Scaling Group of EC2 Instances (handles Docker runs)**.

This simplifies your servers: they no longer need Caddy for SSL, as the ALB will handle the HTTPS certificates automatically.

---

## Step 1: Prepare the "Golden Image" (AMI)
You need a template so AWS can instantly clone new servers.

1. Take your perfectly working, manually configured EC2 instance (where you have already installed Docker, built the language images, and set up the systemd service).
2. Go to the **AWS EC2 Console**.
3. Right-click your instance -> **Image and templates** -> **Create image**.
4. Name it `modigo-runner-base`. 
*Note: This creates an Amazon Machine Image (AMI) which is an exact snapshot of your server.*

---

## Step 2: Create a Launch Template
AWS needs to know what size server to boot up when scaling.

1. In the EC2 Console, go to **Launch Templates** -> **Create launch template**.
2. **Name:** `modigo-runner-template`
3. **AMI:** Choose the `modigo-runner-base` AMI you just created.
4. **Instance Type:** Choose `t3.large` or `t3.xlarge`.
5. **Security Group:** Select a security group that allows inbound traffic on port `8080` *only from your Load Balancer's security group* (this prevents people from bypassing the load balancer).
6. **User Data (Advanced details):** Add a script that automatically pulls the latest code and restarts the service when a new machine boots up. For example:
   ```bash
   #!/bin/bash
   cd /home/ubuntu/modigo/modigo-runner
   git pull origin main
   go build -o modigo-runner .
   sudo cp modigo-runner /usr/local/bin/modigo-runner
   sudo systemctl restart modigo-runner
   ```

---

## Step 3: Create an Application Load Balancer (ALB)
The ALB will distribute students evenly across all running servers.

1. Go to **Load Balancers** -> **Create Load Balancer** -> **Application Load Balancer**.
2. **Listeners:** Add a listener for **HTTPS (443)**.
3. **Certificate:** Use AWS Certificate Manager (ACM) to request a free SSL certificate for `runner.modigo.online`.
4. **Target Group:** Create a Target Group for the instances. 
   - Protocol: HTTP
   - Port: 8080 (the port your Go app runs on natively)
   - Health Check Path: `/health`
5. Point your DNS (in Cloudflare or Route53) for `runner.modigo.online` to this ALB's domain name, rather than a single IP address.

---

## Step 4: Create the Auto Scaling Group (ASG)
This is the brain that adds or removes servers.

1. Go to **Auto Scaling Groups** -> **Create Auto Scaling group**.
2. **Launch Template:** Select `modigo-runner-template`.
3. **Load Balancing:** Attach it to the Target Group you created in Step 3.
4. **Group Size:** 
   - Desired capacity: 1
   - Minimum capacity: 1
   - Maximum capacity: 10 (or however high you want to allow it to scale).
5. **Scaling Policies:** Choose **Target tracking scaling policy**.
   - Metric type: **Average CPU utilization**.
   - Target value: **60%**.
   
### How it works now:
When your class starts and students begin compiling code, the CPU on your single server will start to rise. Once it crosses 60%, AWS will automatically boot up a second server using your AMI template. The Load Balancer will see it pass the `/health` check and start sending half the students to the new server. If CPU continues to stay above 60%, it will boot a third, and so on. When class is over and CPU drops, it will safely terminate the extra servers so you aren't paying for them overnight!
